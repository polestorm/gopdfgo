package sign

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"strconv"
	"testing"
	"time"
)

// fixedTestKey to klucz RSA 2048 WYŁĄCZNIE TESTOWY (ADR 0050 pkt 7), wygenerowany raz poleceniem
// openssl genpkey; służy tylko sprawdzeniu, że wynik poziomu B-B nie zmienia się między rundami.
const fixedTestKey = `-----BEGIN PRIVATE KEY-----
MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQCjKxxWI0/hmhOX
OaeVtctsBEBox2RXydLttsJnDSashcK1EWqxPkZxUllPIv5Y900Y7Y+amk82SAIJ
ONyWq9Ti/qpmHYnwkY5uIAwwhmHOVQNoHwhVQ1zC0ifizOa9Qmg+zHDUsr6gOhnN
9wzSEaP0CUf67C0bzOkcyqWvYyPibyOwWStB+AuuDVEXM2BWO/H+PaUVER2ocIfQ
Q+G8Z+4+QzxrPvlChx5RvJ3WifC3dYois8t6hlXVwxqjKQz/p0hnanoMC/TVUX+f
1wFkOd53vOD5kpkYmPsbMElpsfm+Ab+FIhqDIqJXXNjahbW4kMG2n5B8UCTNQXh4
lZ+RtkcfAgMBAAECggEAAfocQq/0pofmGHsbIJT83A3NATXvdbJkUtZPqVWphabm
qvr4uHwF18R/qc/eWCrF4gvTeA7rOeED7Y7e8f/DgbuOg/Tv7NyYsREGd+GYbkv1
yTAEfS9WFUtgYFeh7uqppZcUB+80C/QsZKRHVtfeU2TSr1lvzlpL850Ys8u0GxZc
TY8cEh2kQvTdz8cYDxoe6bkwZOGEeAv6Mie+TqnIqIC6x/O+d4X4UztiSV5Va5Qi
2S5DWdySUX2lTecv70IWh3BmQ+xgTU7XGbEySvkwy4/VYVxN0s6juJ9mka1tJl5P
tYLSuNZPoQs6YvivMqm0x9vNF7YbBHAoZ/B8DjuWgQKBgQDUcKZ7mhOKlEITZIK/
KyuTh88LF80AzXE45U+ldp2SwBhoMpm9xa2+Rj3HZQa/+uzkq+WVMPfDDPOKhPUo
0o50jS18Ae+0PqnNcdUz+zD3vBWY1IFUpVA6MUM5IiKHhGdk9yOHe29DfyK+k9Tv
e6TT9sDbHb68Mr2GoXo/3QNrxwKBgQDEoBsSw/Xqpwh6BE5s60wUV4hgNOQ/4G18
8iZBMiBabD+RcXIoy97/kFTfuhxVD1ApstlQXwwE+Wjzz0wiKR3zTuT10tjqBNYH
yP4FjkQWiYgx4Pe9NRxiY6Lx+w+zD42+vZlp6j+TN7tzdl8KS9r4JNneFedN6LI4
xtiGo9dZ6QKBgGj4bNsATA2aqneon45Uh4izf5OspAh0ezCw5kUNMlk0ABtkfWEv
D2XIkXg4LDiqhWmMormwY4yGlsRoZDdSqz9eDw6KamY23HJ+f+nagdy3nDhTMMCh
mt3WP7A4dLVfsDNMhsrILDc+1+0xYPMgtlZiQ/NBZGDb9ZhTq/Nogus7AoGAGdaA
OEMP+bnhX0dZlu6EnNez0BCjPdGcEaMwa7jOj5NR9ybVPY8syqCIxq2KM5NDkXcz
H2CeD7lGlWNUOdj1Wpevdq4yWu4vwe4jk09SmzOPdh5975sfE77MFshLE9ijY8E+
e7CQkiPsuguACbZWG+2bw5dVQeOmJ9fZdDfrgKECgYBAyM8y+VLfDpvoYUUiPn18
HGtx4IUUaHL6h+uBw6gUZVUYcU/hKmbikB0h2romA5JuVBbOTfDsEToDC/HzxTIE
MxTIaJXs/WunhO14GdS65UpthAAJKqtKMMJ9RCadT0j0klwLbaXhKybrNJSA3DsX
kBX1qrs8MTuoLZeYZlKn/A==
-----END PRIVATE KEY-----
`

// fixedIdentity to stały klucz testowy i certyfikat samopodpisany zbudowany z niego deterministycznie
// (podpis RSA PKCS#1 v1.5 nie zależy od losowości).
func fixedIdentity(t *testing.T) testIdentity {
	t.Helper()
	block, _ := pem.Decode([]byte(fixedTestKey))
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key := parsed.(*rsa.PrivateKey)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(20261123),
		Subject:               pkix.Name{CommonName: "gopdfgo fixed test signer", Organization: []string{"gopdfgo tests"}},
		NotBefore:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(nil, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testIdentity{key: key, cert: cert}
}

// handWrittenPDF to ręcznie zapisany PDF z tablicą odsyłaczy, istniejącym /AcroForm pośrednim
// i /Annots pośrednim (niezależny od renderowania gopdfgo).
func handWrittenPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595.2756 841.8898] /Contents 4 0 R /Annots 6 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [] /DA (/Helv 0 Tf 0 g) >>",
		"[]",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		b.WriteString(strconv.Itoa(i+1) + " 0 obj\n" + obj + "\nendobj\n")
	}
	xref := b.Len()
	b.WriteString("xref\n0 " + strconv.Itoa(len(objects)+1) + "\n0000000000 65535 f\r\n")
	for _, off := range offsets {
		b.WriteString(leftPad(strconv.Itoa(off), 10) + " 00000 n\r\n")
	}
	b.WriteString("trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n" + strconv.Itoa(xref) + "\n%%EOF")
	return b.Bytes()
}

// roundOneDigest to SHA-256 wyniku SignPDF z rundy 1 (commit 7ce2c7d) dla stałego klucza, stałego
// czasu i handWrittenPDF; poziom domyślny (B-B) ma dawać te same bajty.
const roundOneDigest = "3081df6bdead6a7360eee4af21a7262f62fa3849487c2b63b4fa8ac576cb400c"

func TestLevelBBUnchangedSinceRoundOne(t *testing.T) {
	signed, err := SignPDF(t.Context(), handWrittenPDF(), signOptions(fixedIdentity(t)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(signed)
	if got := hex.EncodeToString(sum[:]); got != roundOneDigest {
		t.Errorf("SHA-256 wyniku B-B %s, w rundzie 1 %s", got, roundOneDigest)
	}
}
