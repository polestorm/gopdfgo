package keyfile

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Klucze i certyfikaty są generowane w teście (wyłącznie testowe). Pliki PKCS#12 powstają własnym
// zapisem tylko do testu (buildPKCS12: PBES2 z AES-256 i MAC SHA-256 jak OpenSSL 3, 3DES i MAC SHA-1
// jak starsze programy) oraz poleceniem openssl pkcs12 -export, jeśli jest dostępne.

// chain tworzy urząd główny, pośredni i certyfikat końcowy dla klucza key.
func chain(t *testing.T, key crypto.Signer) (leaf, intermediate, root *x509.Certificate) {
	t.Helper()
	issue := func(cn string, pub crypto.PublicKey, parent *x509.Certificate, parentKey crypto.Signer, ca bool) *x509.Certificate {
		template := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: ca,
		}
		if parent == nil {
			parent = template
		}
		der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, parentKey)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return cert
	}
	rootKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	midKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	root = issue("test root", rootKey.Public(), nil, rootKey, true)
	intermediate = issue("test intermediate", midKey.Public(), root, rootKey, true)
	leaf = issue("test signer", key.Public(), intermediate, midKey, false)
	return
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	der, err := asn1.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func explicit0(der []byte) asn1.RawValue {
	return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: der}
}

// encryptForTest szyfruje dane jak OpenSSL 3 (modern: PBES2, PBKDF2-HMAC-SHA256, AES-256-CBC) albo
// jak starsze programy (pbeWithSHAAnd3-KeyTripleDES-CBC).
func encryptForTest(t *testing.T, data []byte, password string, modern bool) (pkix.AlgorithmIdentifier, []byte) {
	t.Helper()
	salt := make([]byte, 16)
	rand.Read(salt)
	var block cipher.Block
	var iv []byte
	var alg pkix.AlgorithmIdentifier
	if modern {
		key, _ := pbkdf2.Key(sha256.New, password, salt, 2048, 32)
		iv = make([]byte, 16)
		rand.Read(iv)
		block, _ = aes.NewCipher(key)
		null := asn1.RawValue{Tag: asn1.TagNull}
		kdf := pkix.AlgorithmIdentifier{Algorithm: oidPBKDF2, Parameters: asn1.RawValue{FullBytes: mustMarshal(t,
			pbkdf2Params{Salt: salt, Iterations: 2048, PRF: pkix.AlgorithmIdentifier{Algorithm: oidHMACWithSHA256, Parameters: null}})}}
		scheme := pkix.AlgorithmIdentifier{Algorithm: oidAES256CBC, Parameters: asn1.RawValue{FullBytes: mustMarshal(t, iv)}}
		alg = pkix.AlgorithmIdentifier{Algorithm: oidPBES2, Parameters: asn1.RawValue{FullBytes: mustMarshal(t, pbes2Params{KDF: kdf, Scheme: scheme})}}
	} else {
		salt = salt[:8]
		bmp := bmpString(password)
		key := pkcs12KDF(sha1.New, 20, 64, salt, bmp, 2048, 1, 24)
		iv = pkcs12KDF(sha1.New, 20, 64, salt, bmp, 2048, 2, 8)
		block, _ = des.NewTripleDESCipher(key)
		alg = pkix.AlgorithmIdentifier{Algorithm: oidPBEWithSHA3KeyDES, Parameters: asn1.RawValue{FullBytes: mustMarshal(t, pbeParams{Salt: salt, Iterations: 2048})}}
	}
	n := block.BlockSize() - len(data)%block.BlockSize()
	padded := append(append([]byte(nil), data...), make([]byte, n)...)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(n)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return alg, out
}

// buildPKCS12 zapisuje plik PKCS#12 (tylko do testu): klucz w pkcs8ShroudedKeyBag, certyfikaty
// w zaszyfrowanym SafeContents, MAC SHA-256 (modern) albo SHA-1.
func buildPKCS12(t *testing.T, key crypto.Signer, certs []*x509.Certificate, password string, modern bool) []byte {
	t.Helper()
	keyID := []byte{1, 2, 3, 4}
	idAttr := []bagAttribute{{ID: oidLocalKeyID, Values: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet,
		IsCompound: true, Bytes: mustMarshal(t, keyID)}}}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	alg, encrypted := encryptForTest(t, pkcs8, password, modern)
	keyBag := safeBag{ID: oidShroudedKeyBag, Value: explicit0(mustMarshal(t,
		encryptedPrivateKeyInfo{Algorithm: alg, EncryptedData: encrypted})), Attributes: idAttr}
	var certBags []safeBag
	for i, cert := range certs {
		bag := safeBag{ID: oidCertBag, Value: explicit0(mustMarshal(t, certBag{ID: oidX509Certificate, Data: cert.Raw}))}
		if i == 0 {
			bag.Attributes = idAttr
		}
		certBags = append(certBags, bag)
	}
	certAlg, certData := encryptForTest(t, mustMarshal(t, certBags), password, modern)
	type encryptedContent struct {
		ContentType                asn1.ObjectIdentifier
		ContentEncryptionAlgorithm pkix.AlgorithmIdentifier
		EncryptedContent           []byte `asn1:"tag:0"`
	}
	type encryptedDataOut struct {
		Version              int
		EncryptedContentInfo encryptedContent
	}
	ed := mustMarshal(t, encryptedDataOut{EncryptedContentInfo: encryptedContent{oidData, certAlg, certData}})
	safes := []contentInfo{
		{ContentType: oidEncryptedData, Content: explicit0(ed)},
		{ContentType: oidData, Content: explicit0(mustMarshal(t, mustMarshal(t, []safeBag{keyBag})))},
	}
	authSafe := mustMarshal(t, safes)
	salt := make([]byte, 16)
	rand.Read(salt)
	digest, size, newHash := oidSHA1, 20, sha1.New
	if modern {
		digest, size, newHash = oidSHA256, 32, sha256.New
	}
	mac := hmac.New(newHash, pkcs12KDF(newHash, size, 64, salt, bmpString(password), 2048, 3, size))
	mac.Write(authSafe)
	return mustMarshal(t, pfx{Version: 3, AuthSafe: contentInfo{ContentType: oidData, Content: explicit0(mustMarshal(t, authSafe))},
		MacData: macData{Mac: digestInfo{Algorithm: pkix.AlgorithmIdentifier{Algorithm: digest, Parameters: asn1.NullRawValue}, Digest: mac.Sum(nil)},
			MacSalt: salt, Iterations: 2048}})
}

func checkIdentity(t *testing.T, id *Identity, key crypto.Signer, leaf, intermediate, root *x509.Certificate) {
	t.Helper()
	if !id.Signer.Public().(interface{ Equal(crypto.PublicKey) bool }).Equal(key.Public()) {
		t.Error("klucz inny niż zapisany")
	}
	if !id.Certificate.Equal(leaf) {
		t.Errorf("certyfikat %s, oczekiwany %s", id.Certificate.Subject, leaf.Subject)
	}
	if len(id.Chain) != 2 || !id.Chain[0].Equal(intermediate) || !id.Chain[1].Equal(root) {
		t.Errorf("łańcuch %d certyfikatów, oczekiwane: pośredni, główny", len(id.Chain))
	}
}

func TestPKCS12OwnWriter(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	for _, key := range []crypto.Signer{rsaKey, ecKey} {
		leaf, intermediate, root := chain(t, key)
		for _, modern := range []bool{true, false} {
			for _, password := range []string{"tajne hasło ąę", ""} {
				// certyfikaty w kolejności innej niż ścieżka: łańcuch ma być uporządkowany
				data := buildPKCS12(t, key, []*x509.Certificate{leaf, root, intermediate}, password, modern)
				id, err := ParsePKCS12(data, password)
				if err != nil {
					t.Fatalf("modern=%v hasło %q: %v", modern, password, err)
				}
				checkIdentity(t, id, key, leaf, intermediate, root)
				if _, err := ParsePKCS12(data, password+"x"); !errors.Is(err, ErrPassword) {
					t.Errorf("złe hasło, modern=%v: %v", modern, err)
				}
			}
		}
	}
	if _, err := ParsePKCS12([]byte("nie PKCS#12"), ""); !errors.Is(err, ErrMalformed) {
		t.Errorf("nie PKCS#12: %v", err)
	}
}

// openssl pkcs12 -export w wariantach domyślnym (OpenSSL 3: PBES2, AES-256-CBC, MAC SHA-256),
// starszym (3DES, MAC SHA-1), PBMAC1 (RFC 9579) i -legacy (certyfikaty RC2-40: jasny błąd).
func TestPKCS12FromOpenSSL(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("brak openssl")
	}
	dir := t.TempDir()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	leaf, intermediate, root := chain(t, key)
	write := func(name string, blocks ...*pem.Block) string {
		var data []byte
		for _, b := range blocks {
			data = append(data, pem.EncodeToMemory(b)...)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPath := write("key.pem", &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	certPath := write("cert.pem", &pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	chainPath := write("chain.pem", &pem.Block{Type: "CERTIFICATE", Bytes: intermediate.Raw}, &pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	for _, c := range []struct {
		name string
		args []string
		want error
	}{
		{"default", nil, nil},
		{"3des-sha1", []string{"-keypbe", "PBE-SHA1-3DES", "-certpbe", "PBE-SHA1-3DES", "-macalg", "sha1"}, nil},
		{"pbmac1", []string{"-pbmac1_pbkdf2"}, nil},
		{"legacy", []string{"-legacy"}, ErrUnsupportedAlgorithm},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := filepath.Join(dir, c.name+".p12")
			args := append([]string{"pkcs12", "-export", "-inkey", keyPath, "-in", certPath, "-certfile", chainPath,
				"-out", out, "-passout", "pass:hasło"}, c.args...)
			if output, err := exec.Command(openssl, args...).CombinedOutput(); err != nil {
				t.Skipf("openssl %v: %v\n%s", c.args, err, output)
			}
			data, _ := os.ReadFile(out)
			id, err := ParsePKCS12(data, "hasło")
			if c.want != nil {
				if !errors.Is(err, c.want) || !strings.Contains(err.Error(), "-certpbe AES-256-CBC") {
					t.Errorf("błąd %v, oczekiwany %v z poleceniem konwersji", err, c.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			checkIdentity(t, id, key, leaf, intermediate, root)
			if _, err := ParsePKCS12(data, "inne"); !errors.Is(err, ErrPassword) {
				t.Errorf("złe hasło: %v", err)
			}
		})
	}
}

func TestPEMKeys(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(ecKey)
	sec1, _ := x509.MarshalECPrivateKey(ecKey)
	cases := []struct {
		name string
		data []byte
		key  crypto.Signer
		want error
	}{
		{"pkcs8", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), ecKey, nil},
		{"pkcs1", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)}), rsaKey, nil},
		{"sec1", append(pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: []byte{6, 5, 43, 129, 4, 0, 34}}),
			pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1})...), ecKey, nil},
		{"encrypted-pkcs8", pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{0x30, 0}}), nil, ErrEncryptedPEM},
		{"encrypted-legacy", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY",
			Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-256-CBC,00"}, Bytes: []byte{0}}), nil, ErrEncryptedPEM},
		{"certificate-only", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{0}}), nil, ErrNoKey},
	}
	for _, c := range cases {
		key, err := ParsePEMKey(c.data)
		if c.want != nil {
			if !errors.Is(err, c.want) {
				t.Errorf("%s: błąd %v, oczekiwany %v", c.name, err, c.want)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !key.Public().(interface{ Equal(crypto.PublicKey) bool }).Equal(c.key.Public()) {
			t.Errorf("%s: inny klucz", c.name)
		}
	}
	leaf, intermediate, root := chain(t, ecKey)
	certs := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	chainPEM := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: intermediate.Raw})...)
	id, err := ParsePEM(cases[0].data, certs, chainPEM)
	if err != nil {
		t.Fatal(err)
	}
	checkIdentity(t, id, ecKey, leaf, intermediate, root)
	if _, err := ParsePEM(cases[1].data, certs, nil); !errors.Is(err, ErrNoCertificate) {
		t.Errorf("certyfikat innego klucza: %v", err)
	}
}
