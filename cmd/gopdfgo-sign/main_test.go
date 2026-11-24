package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/polestorm/gopdfgo/htmlpdf"
	"github.com/polestorm/gopdfgo/sign"
	"github.com/polestorm/gopdfgo/sign/keyfile"
)

// Polecenie gopdfgo-sign na PDF z gopdfgo: klucz z pliku PKCS#12 (z openssl pkcs12 -export, jeśli jest:
// wariant domyślny OpenSSL 3 i 3DES z MAC SHA-1) i z plików PEM; wynik sprawdza pdfsig. Klucze
// i certyfikaty są generowane w teście (wyłącznie testowe). Żadnych połączeń poza 127.0.0.1.

type fixture struct {
	dir                     string
	pdf                     string // PDF z gopdfgo z pustym polem Podpis1
	key, cert, chain, combo string // PEM: klucz PKCS#1, certyfikat, łańcuch, klucz z certyfikatem
	root                    *x509.Certificate
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	font, err := os.ReadFile("../../testdata/fonts/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	html := `<!DOCTYPE html><html lang="pl"><head><title>Umowa</title></head><body><h1>Umowa</h1>
<div data-pdf-signature="Podpis1" style="width: 6cm; height: 2cm; border: 1px solid">Podpis</div></body></html>`
	result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{
		Fonts: []htmlpdf.Font{{Name: "NotoSans-Regular.ttf", Data: font}}, Conformance: htmlpdf.PDFA2b})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{dir: dir, pdf: filepath.Join(dir, "in.pdf")}
	os.WriteFile(f.pdf, result.PDF, 0o644)

	rootKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	issue := func(cn string, pub crypto.PublicKey, parent *x509.Certificate, signer crypto.Signer, ca bool) *x509.Certificate {
		template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), BasicConstraintsValid: true, IsCA: ca,
			KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment | x509.KeyUsageCertSign}
		if parent == nil {
			parent = template
		}
		der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, signer)
		if err != nil {
			t.Fatal(err)
		}
		cert, _ := x509.ParseCertificate(der)
		return cert
	}
	f.root = issue("gopdfgo-sign test root", rootKey.Public(), nil, rootKey, true)
	leaf := issue("gopdfgo-sign test signer", key.Public(), f.root, rootKey, false)
	write := func(name string, blocks ...*pem.Block) string {
		var data []byte
		for _, b := range blocks {
			data = append(data, pem.EncodeToMemory(b)...)
		}
		path := filepath.Join(dir, name)
		os.WriteFile(path, data, 0o600)
		return path
	}
	keyBlock := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	f.key = write("key.pem", keyBlock)
	f.cert = write("cert.pem", &pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	f.chain = write("chain.pem", &pem.Block{Type: "CERTIFICATE", Bytes: f.root.Raw})
	f.combo = write("combo.pem", keyBlock, &pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	return f
}

// pdfsigValid sprawdza wynik programem pdfsig (Poppler), jeśli jest dostępny.
func pdfsigValid(t *testing.T, path string, signatures int) {
	t.Helper()
	pdfsig, err := exec.LookPath("pdfsig")
	if err != nil {
		t.Log("brak pdfsig, sprawdzenie pominięte")
		return
	}
	out, _ := exec.Command(pdfsig, "-nocert", path).CombinedOutput()
	if n := strings.Count(string(out), "Signature Validation: Signature is Valid."); n != signatures {
		t.Errorf("pdfsig: ważnych podpisów %d z %d\n%s", n, signatures, out)
	}
	if !strings.Contains(string(out), "Podpis1") && signatures == 1 {
		t.Errorf("pdfsig: podpis poza polem Podpis1\n%s", out)
	}
}

func TestSignCommandPEM(t *testing.T) {
	f := newFixture(t)
	out := filepath.Join(f.dir, "out.pdf")
	var stderr bytes.Buffer
	err := run([]string{"-in", f.pdf, "-out", out, "-key", f.key, "-cert", f.cert, "-chain", f.chain,
		"-field", "Podpis1", "-reason", "Zatwierdzenie", "-location", "Kraków", "-name", "Jan Kowalski", "-contact", "jan@example.com"},
		func(string) string { return "" }, &stderr)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	in, _ := os.ReadFile(f.pdf)
	signed, _ := os.ReadFile(out)
	if !bytes.HasPrefix(signed, in) || !bytes.Contains(signed[len(in):], []byte("/Reason")) {
		t.Fatal("wynik nie jest aktualizacją przyrostową pliku wejściowego z podpisem")
	}
	pdfsigValid(t, out, 1)
	// klucz i certyfikat w jednym pliku PEM; podpis drugi w nowym polu niewidocznym
	out2 := filepath.Join(f.dir, "out2.pdf")
	if err := run([]string{"-in", out, "-out", out2, "-key", f.combo, "-cert", f.combo}, os.Getenv, &stderr); err != nil {
		t.Fatal(err)
	}
	pdfsigValid(t, out2, 2)
}

func TestSignCommandPKCS12(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("brak openssl: pliki PKCS#12 sprawdza też sign/keyfile własnym zapisem")
	}
	f := newFixture(t)
	passwordFile := filepath.Join(f.dir, "password.txt")
	os.WriteFile(passwordFile, []byte("hasło p12\r\n"), 0o600)
	for _, c := range []struct {
		name string
		args []string
	}{
		{"openssl3-default", nil},
		{"3des-sha1", []string{"-keypbe", "PBE-SHA1-3DES", "-certpbe", "PBE-SHA1-3DES", "-macalg", "sha1"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p12 := filepath.Join(f.dir, c.name+".p12")
			args := append([]string{"pkcs12", "-export", "-inkey", f.key, "-in", f.cert, "-certfile", f.chain,
				"-out", p12, "-passout", "pass:hasło p12"}, c.args...)
			if output, err := exec.Command(openssl, args...).CombinedOutput(); err != nil {
				t.Skipf("openssl: %v\n%s", err, output)
			}
			env := func(name string) string {
				if name == "P12_PASSWORD" {
					return "hasło p12"
				}
				return ""
			}
			for i, password := range [][]string{{"-password-env", "P12_PASSWORD"}, {"-password-file", passwordFile}} {
				out := filepath.Join(f.dir, c.name+string(rune('a'+i))+".pdf")
				args := append([]string{"-in", f.pdf, "-out", out, "-p12", p12, "-field", "Podpis1"}, password...)
				var stderr bytes.Buffer
				if err := run(args, env, &stderr); err != nil {
					t.Fatalf("%v: %v\n%s", password, err, stderr.String())
				}
				pdfsigValid(t, out, 1)
			}
			err := run([]string{"-in", f.pdf, "-out", filepath.Join(f.dir, "bad.pdf"), "-p12", p12}, env, io.Discard)
			if !errors.Is(err, keyfile.ErrPassword) || !strings.Contains(err.Error(), "-password-env") {
				t.Errorf("bez hasła: %v", err)
			}
		})
	}
}

// Uruchomienie zbudowanej binarki: kod wyjścia, brak flagi z hasłem w wierszu poleceń.
func TestSignCommandBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("buduje binarkę")
	}
	f := newFixture(t)
	binary := filepath.Join(f.dir, "gopdfgo-sign")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	out := filepath.Join(f.dir, "out.pdf")
	if output, err := exec.Command(binary, "-in", f.pdf, "-out", out, "-key", f.key, "-cert", f.cert, "-field", "Podpis1").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	pdfsigValid(t, out, 1)
	cmd := exec.Command(binary, "-in", f.pdf, "-out", out, "-p12", "x.p12", "-password", "tajne")
	output, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 2 || !strings.Contains(string(output), "-password") {
		t.Errorf("flaga -password: kod %d\n%s", cmd.ProcessState.ExitCode(), output)
	}
}

func TestSignCommandErrors(t *testing.T) {
	f := newFixture(t)
	out := filepath.Join(f.dir, "out.pdf")
	base := []string{"-in", f.pdf, "-out", out, "-key", f.key, "-cert", f.cert}
	cases := []struct {
		args  []string
		usage bool
		want  error
	}{
		{[]string{"-in", f.pdf}, true, nil},
		{append(base, "-level", "B-T"), true, nil},
		{append(base, "-level", "B-LT"), true, nil},
		{append(base, "-level", "B-X"), true, nil},
		{append(base, "-p12", "x.p12"), true, nil},
		{append(base, "-password-env", "X"), true, nil},
		{[]string{"-in", f.pdf, "-out", out, "-key", f.key}, true, nil},
		{append(base, "-field", "Brak"), false, sign.ErrUnknownField},
		{[]string{"-in", f.pdf, "-out", out, "-key", f.key, "-cert", f.chain}, false, keyfile.ErrNoCertificate},
	}
	for i, c := range cases {
		err := run(c.args, func(string) string { return "" }, io.Discard)
		var usage usageError
		if err == nil || errors.As(err, &usage) != c.usage || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("przypadek %d %q: %v", i, c.args, err)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("plik wynikowy powstał mimo błędu")
	}

	// B-T łączy się tylko z adresem -tsa (tu serwer lokalny odmawiający usługi)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "niedostępny", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	err := run(append(base, "-level", "B-T", "-tsa", server.URL), os.Getenv, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "503") || requests.Load() != 1 {
		t.Errorf("B-T z TSA odmawiającym: %v, żądań %d", err, requests.Load())
	}
}
