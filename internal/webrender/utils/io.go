package utils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"golang.org/x/net/html/charset"
)

type ContentInput interface {
	isContentInput()
	String() string
}

type (
	InputFilename string
	InputUrl      string
	InputString   string
	InputReader   struct {
		io.ReadCloser
	}
)

func (c InputFilename) isContentInput() {}
func (c InputUrl) isContentInput()      {}
func (c InputString) isContentInput()   {}
func (c InputReader) isContentInput()   {}
func (c InputFilename) String() string {
	return string(c)
}

func (c InputUrl) String() string {
	if strings.HasPrefix(string(c), "data:") {
		return fmt.Sprintf("data url (%d B)", len(c))
	}
	return string(c)
}

func (c InputString) String() string {
	return fmt.Sprintf("string (%d B)", len(c))
}

// InputStylesheetUrl (gopdfgo) to adres arkusza stylów z kodowaniem środowiska (CSS Syntax 3 § 3.2,
// krok 3), np. z atrybutu charset elementu <link>.
type InputStylesheetUrl struct {
	Url, EnvironmentEncoding string
}

func (c InputStylesheetUrl) isContentInput() {}
func (c InputStylesheetUrl) String() string  { return InputUrl(c.Url).String() }

func (c InputReader) String() string {
	return fmt.Sprintf("reader of type %T", c.ReadCloser)
}

type Source struct {
	BaseUrl string
	Content []byte // utf8 encoded
}

// FetchSource fetch the html input, and returns it with the
// normalized “BaseUrl“ (checkCssMimeType=false).
func FetchSource(input ContentInput, baseUrl string, urlFetcher UrlFetcher,
	checkCssMimeType bool,
) (out Source, err error) {
	return fetchSource(input, baseUrl, urlFetcher, checkCssMimeType, false)
}

// FetchStylesheet (gopdfgo) działa jak FetchSource, ale treść pobraną spod
// adresu dekoduje według reguł dla arkuszy stylów (decodeStylesheet), także
// wtedy, gdy typ MIME nie jest sprawdzany (@import).
func FetchStylesheet(input ContentInput, baseUrl string, urlFetcher UrlFetcher,
	checkCssMimeType bool,
) (out Source, err error) {
	return fetchSource(input, baseUrl, urlFetcher, checkCssMimeType, true)
}

func fetchSource(input ContentInput, baseUrl string, urlFetcher UrlFetcher,
	checkCssMimeType, stylesheet bool,
) (out Source, err error) {
	if baseUrl != "" {
		baseUrl, err = ensureUrl(baseUrl)
		if err != nil {
			return
		}
	}
	environmentEncoding := ""
	if data, ok := input.(InputStylesheetUrl); ok {
		input, environmentEncoding = InputUrl(data.Url), data.EnvironmentEncoding
	}
	switch data := input.(type) {
	case InputFilename:
		if baseUrl == "" {
			baseUrl, err = PathToURL(string(data))
			if err != nil {
				return
			}
		}
		f, err := os.ReadFile(string(data))
		if err != nil {
			return Source{}, err
		}
		return Source{Content: f, BaseUrl: baseUrl}, nil
	case InputUrl:
		result, err := urlFetcher(string(data))
		if err != nil {
			return Source{}, err
		}
		if result.RedirectedUrl == "" {
			result.RedirectedUrl = string(data)
		}
		if checkCssMimeType && result.MimeType != "text/css" {
			logger.WarningLogger.Printf("Unsupported stylesheet type %s for %s",
				result.MimeType, result.RedirectedUrl)
			return Source{BaseUrl: baseUrl}, nil
		} else {
			if baseUrl == "" {
				baseUrl = result.RedirectedUrl
			}
			var decoded []byte
			if stylesheet {
				decoded, err = decodeStylesheet(result.Content, result.ProtocolEncoding, environmentEncoding)
			} else {
				decoded, err = decodeToUtf8(result.Content, result.ProtocolEncoding)
			}
			if err != nil {
				return Source{}, err
			}
			return Source{Content: decoded, BaseUrl: baseUrl}, nil
		}
	case InputReader:
		bt, err := io.ReadAll(data.ReadCloser)
		if err != nil {
			return Source{}, err
		}
		if err = data.ReadCloser.Close(); err != nil {
			return Source{}, err
		}
		return Source{Content: bt, BaseUrl: baseUrl}, nil
	case InputString:
		return Source{Content: []byte(data), BaseUrl: baseUrl}, nil
	default:
		return Source{}, fmt.Errorf("unexpected input type %T", input)
	}
}

func decodeToUtf8(data io.Reader, encod string) ([]byte, error) {
	if encod == "" { // assume UTF8
		return io.ReadAll(data)
	}
	enc, _ := charset.Lookup(encod)
	if enc == nil {
		return nil, fmt.Errorf("unsupported encoding %s", encod)
	}
	return io.ReadAll(enc.NewDecoder().Reader(data))
}

// decodeStylesheet (gopdfgo) ustala kodowanie arkusza według CSS Syntax 3
// § 3.2: znacznik kolejności bajtów, potem kodowanie z protokołu, potem
// reguła @charset zapisana dosłownie na początku pliku, potem kodowanie
// środowiska (na razie tylko atrybut charset elementu <link>, jak w Chromium;
// kodowania dokumentu nie znamy, bo dokument jest zawsze czytany jako UTF-8),
// na końcu UTF-8. Źródło patrzyło tylko na protokół.
func decodeStylesheet(data io.Reader, protocolEncoding, environmentEncoding string) ([]byte, error) {
	content, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}
	switch {
	case bytes.HasPrefix(content, []byte{0xEF, 0xBB, 0xBF}):
		return content[3:], nil
	case bytes.HasPrefix(content, []byte{0xFE, 0xFF}):
		return decodeToUtf8(bytes.NewReader(content[2:]), "utf-16be")
	case bytes.HasPrefix(content, []byte{0xFF, 0xFE}):
		return decodeToUtf8(bytes.NewReader(content[2:]), "utf-16le")
	}
	if enc, _ := charset.Lookup(protocolEncoding); enc != nil {
		return io.ReadAll(enc.NewDecoder().Reader(bytes.NewReader(content)))
	}
	const prefix = `@charset "`
	if head := content[:min(len(content), 1024)]; bytes.HasPrefix(head, []byte(prefix)) {
		if end := bytes.Index(head[len(prefix):], []byte(`";`)); end >= 0 {
			enc, name := charset.Lookup(string(head[len(prefix) : len(prefix)+end]))
			// Arkusz, w którym regułę dało się odczytać bajt po bajcie, nie
			// może być zapisany w UTF-16; norma każe wtedy przyjąć UTF-8.
			if enc != nil && !strings.HasPrefix(name, "utf-16") && name != "utf-8" {
				return io.ReadAll(enc.NewDecoder().Reader(bytes.NewReader(content)))
			}
		}
	}
	if enc, _ := charset.Lookup(environmentEncoding); enc != nil {
		return io.ReadAll(enc.NewDecoder().Reader(bytes.NewReader(content)))
	}
	return content, nil
}
