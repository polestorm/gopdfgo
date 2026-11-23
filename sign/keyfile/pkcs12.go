package keyfile

// Odczyt plików PKCS#12 (RFC 7292) wyłącznie biblioteką standardową Go.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"hash"
	"unicode/utf16"
)

var (
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidEncryptedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 6}

	oidKeyBag             = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 1}
	oidShroudedKeyBag     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 2}
	oidCertBag            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 3}
	oidX509Certificate    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 22, 1}
	oidLocalKeyID         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 21}
	oidPBES2              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2             = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}
	oidPBMAC1             = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 14}
	oidPBEWithSHA3KeyDES  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 3}
	oidPBEWithSHA2KeyDES  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 4}
	oidPBEWithSHALegacy   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1} // RC4 (1, 2), RC2 (5, 6)
	oidAES128CBC          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidDESEDE3CBC         = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}
	oidHMACWithSHA1       = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	oidHMACWithSHA256     = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	oidHMACWithSHA384     = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 10}
	oidHMACWithSHA512     = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}
	oidSHA1               = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256             = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384             = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512             = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	maxIterations         = 10_000_000 // ochrona przed plikiem, którego odczyt trwałby godzinami
	legacyConversionHints = "przekonwertuj plik: openssl pkcs12 -legacy -in stary.p12 -nodes -out tymczasowy.pem " +
		"&& openssl pkcs12 -export -in tymczasowy.pem -out nowy.p12 -certpbe AES-256-CBC -keypbe AES-256-CBC -macalg SHA256 " +
		"(tymczasowy.pem zawiera niezaszyfrowany klucz: usuń go potem)"
)

// Struktury ASN.1 z RFC 7292 § 4 i RFC 8018 (PBES2, PBKDF2) oraz RFC 9579 (PBMAC1).
type (
	pfx struct {
		Version  int
		AuthSafe contentInfo
		MacData  macData `asn1:"optional"`
	}
	contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"optional"` // [0] EXPLICIT, rozpakowywane przez explicitInner
	}
	macData struct {
		Mac        digestInfo
		MacSalt    []byte
		Iterations int `asn1:"optional,default:1"`
	}
	digestInfo struct {
		Algorithm pkix.AlgorithmIdentifier
		Digest    []byte
	}
	encryptedData struct {
		Version              int
		EncryptedContentInfo struct {
			ContentType                asn1.ObjectIdentifier
			ContentEncryptionAlgorithm pkix.AlgorithmIdentifier
			EncryptedContent           asn1.RawValue `asn1:"tag:0,optional"`
		}
	}
	safeBag struct {
		ID         asn1.ObjectIdentifier
		Value      asn1.RawValue  // [0] EXPLICIT, rozpakowywane przez explicitInner
		Attributes []bagAttribute `asn1:"set,optional"`
	}
	bagAttribute struct {
		ID     asn1.ObjectIdentifier
		Values asn1.RawValue `asn1:"set"`
	}
	certBag struct {
		ID   asn1.ObjectIdentifier
		Data []byte `asn1:"tag:0,explicit"`
	}
	encryptedPrivateKeyInfo struct {
		Algorithm     pkix.AlgorithmIdentifier
		EncryptedData []byte
	}
	pbes2Params struct {
		KDF    pkix.AlgorithmIdentifier
		Scheme pkix.AlgorithmIdentifier
	}
	pbkdf2Params struct {
		Salt       []byte
		Iterations int
		KeyLength  int                      `asn1:"optional"`
		PRF        pkix.AlgorithmIdentifier `asn1:"optional"`
	}
	pbeParams struct {
		Salt       []byte
		Iterations int
	}
	pbmac1Params struct {
		KDF    pkix.AlgorithmIdentifier
		Scheme pkix.AlgorithmIdentifier
	}
)

// ParsePKCS12 odczytuje z pliku PKCS#12 (RFC 7292) klucz prywatny, jego certyfikat i pozostałe
// certyfikaty. Obsługiwane: integralność hasłem (MAC HMAC z kluczem z KDF PKCS#12, SHA-1 i SHA-2,
// albo PBMAC1, RFC 9579), szyfrowanie PBES2 z PBKDF2 (AES-CBC, 3DES-CBC; domyślne w OpenSSL 3)
// i pbeWithSHAAnd3-KeyTripleDES-CBC (domyślne w starszych programach). Szyfry RC2 i RC4 (np.
// certyfikaty w plikach z OpenSSL 1.x) dają ErrUnsupportedAlgorithm z poleceniem konwersji.
func ParsePKCS12(data []byte, password string) (*Identity, error) {
	var p pfx
	if rest, err := asn1.Unmarshal(data, &p); err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("%w: PKCS#12: %v", ErrMalformed, errOr(err, "dane za strukturą PFX"))
	}
	if p.Version != 3 {
		return nil, fmt.Errorf("%w: PKCS#12 w wersji %d", ErrMalformed, p.Version)
	}
	if !p.AuthSafe.ContentType.Equal(oidData) {
		return nil, fmt.Errorf("%w: PKCS#12 z integralnością kluczem publicznym (signedData)", ErrUnsupportedAlgorithm)
	}
	authSafe, err := explicitOctets(p.AuthSafe.Content)
	if err != nil {
		return nil, err
	}
	// hasło PKCS#12 KDF to BMPString z zakończeniem; puste hasło bywa zapisane jako dwa zera albo bez
	// bajtów (RFC 7292 dodatek B.1), więc sprawdzamy oba
	passwords := [][]byte{bmpString(password)}
	if password == "" {
		passwords = append(passwords, nil)
	}
	if p.MacData.Mac.Algorithm.Algorithm != nil {
		ok := false
		for i, candidate := range passwords {
			match, err := verifyMAC(&p.MacData, authSafe, candidate, password)
			if err != nil {
				return nil, err
			}
			if match {
				ok, passwords = true, passwords[i:i+1]
				break
			}
		}
		if !ok {
			return nil, ErrPassword
		}
	}

	var safes []contentInfo
	if rest, err := asn1.Unmarshal(authSafe, &safes); err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("%w: AuthenticatedSafe: %v", ErrMalformed, errOr(err, "dane za strukturą"))
	}
	var bags []safeBag
	for _, ci := range safes {
		var contents []byte
		switch {
		case ci.ContentType.Equal(oidData):
			if contents, err = explicitOctets(ci.Content); err != nil {
				return nil, err
			}
		case ci.ContentType.Equal(oidEncryptedData):
			var ed encryptedData
			inner, err := explicitInner(ci.Content)
			if err != nil {
				return nil, err
			}
			if _, err := asn1.Unmarshal(inner.FullBytes, &ed); err != nil {
				return nil, fmt.Errorf("%w: EncryptedData: %v", ErrMalformed, err)
			}
			encrypted, err := octets(ed.EncryptedContentInfo.EncryptedContent)
			if err != nil {
				return nil, err
			}
			if contents, err = decrypt(ed.EncryptedContentInfo.ContentEncryptionAlgorithm, encrypted, password, passwords[0]); err != nil {
				return nil, fmt.Errorf("certyfikaty: %w", err)
			}
		default:
			return nil, fmt.Errorf("%w: zawartość AuthenticatedSafe %v", ErrUnsupportedAlgorithm, ci.ContentType)
		}
		var list []safeBag
		if rest, err := asn1.Unmarshal(contents, &list); err != nil || len(rest) != 0 {
			return nil, fmt.Errorf("%w: SafeContents: %v", ErrMalformed, errOr(err, "dane za strukturą"))
		}
		bags = append(bags, list...)
	}

	type keyEntry struct {
		key any
		id  []byte
	}
	var keys []keyEntry
	var certs []*x509.Certificate
	certIDs := map[*x509.Certificate][]byte{}
	for _, bag := range bags {
		id := localKeyID(bag.Attributes)
		value, err := explicitInner(bag.Value)
		if err != nil {
			return nil, err
		}
		switch {
		case bag.ID.Equal(oidKeyBag), bag.ID.Equal(oidShroudedKeyBag):
			der := value.FullBytes
			if bag.ID.Equal(oidShroudedKeyBag) {
				var info encryptedPrivateKeyInfo
				if _, err := asn1.Unmarshal(der, &info); err != nil {
					return nil, fmt.Errorf("%w: EncryptedPrivateKeyInfo: %v", ErrMalformed, err)
				}
				if der, err = decrypt(info.Algorithm, info.EncryptedData, password, passwords[0]); err != nil {
					return nil, fmt.Errorf("klucz: %w", err)
				}
			}
			key, err := x509.ParsePKCS8PrivateKey(der)
			if err != nil {
				return nil, fmt.Errorf("%w: klucz PKCS#8: %v", ErrMalformed, err)
			}
			keys = append(keys, keyEntry{key, id})
		case bag.ID.Equal(oidCertBag):
			var cb certBag
			if _, err := asn1.Unmarshal(value.FullBytes, &cb); err != nil {
				return nil, fmt.Errorf("%w: CertBag: %v", ErrMalformed, err)
			}
			if !cb.ID.Equal(oidX509Certificate) {
				continue // np. certyfikat SDSI: pomijany
			}
			cert, err := x509.ParseCertificate(cb.Data)
			if err != nil {
				return nil, fmt.Errorf("%w: certyfikat: %v", ErrMalformed, err)
			}
			certs = append(certs, cert)
			certIDs[cert] = id
		}
		// inne torby (CRL, sekrety, zagnieżdżone SafeContents) nie są potrzebne do podpisu
	}
	switch len(keys) {
	case 0:
		return nil, ErrNoKey
	case 1:
	default:
		return nil, fmt.Errorf("%w: plik zawiera %d klucze prywatne", ErrAmbiguous, len(keys))
	}
	// certyfikat klucza: ten sam localKeyId, a bez niego ten sam klucz publiczny
	var byID []*x509.Certificate
	for _, cert := range certs {
		if len(keys[0].id) != 0 && bytes.Equal(certIDs[cert], keys[0].id) {
			byID = append(byID, cert)
		}
	}
	return assemble(keys[0].key, byID, certs)
}

// verifyMAC sprawdza MAC pliku (RFC 7292 § 5 i dodatek B, albo PBMAC1 według RFC 9579 § 3).
func verifyMAC(m *macData, authSafe, bmpPassword []byte, password string) (bool, error) {
	if m.Iterations < 1 || m.Iterations > maxIterations {
		return false, fmt.Errorf("%w: MAC z %d iteracjami", ErrMalformed, m.Iterations)
	}
	var key []byte
	var newHash func() hash.Hash
	if m.Mac.Algorithm.Algorithm.Equal(oidPBMAC1) {
		var params pbmac1Params
		if _, err := asn1.Unmarshal(m.Mac.Algorithm.Parameters.FullBytes, &params); err != nil {
			return false, fmt.Errorf("%w: PBMAC1: %v", ErrMalformed, err)
		}
		if newHash = hmacHash(params.Scheme.Algorithm); newHash == nil {
			return false, fmt.Errorf("%w: PBMAC1 z %v", ErrUnsupportedAlgorithm, params.Scheme.Algorithm)
		}
		kdf, err := pbkdf2Of(params.KDF)
		if err != nil {
			return false, err
		}
		if kdf.KeyLength <= 0 {
			return false, fmt.Errorf("%w: PBMAC1 bez długości klucza", ErrMalformed)
		}
		if key, err = pbkdf2.Key(kdf.hash, password, kdf.Salt, kdf.Iterations, kdf.KeyLength); err != nil {
			return false, err
		}
	} else {
		var size, block int
		newHash, size, block = digestHash(m.Mac.Algorithm.Algorithm)
		if newHash == nil {
			return false, fmt.Errorf("%w: MAC z %v", ErrUnsupportedAlgorithm, m.Mac.Algorithm.Algorithm)
		}
		key = pkcs12KDF(newHash, size, block, m.MacSalt, bmpPassword, m.Iterations, 3, size)
	}
	mac := hmac.New(newHash, key)
	mac.Write(authSafe)
	return subtle.ConstantTimeCompare(mac.Sum(nil), m.Mac.Digest) == 1, nil
}

// pbkdf2Config to odczytane parametry PBKDF2.
type pbkdf2Config struct {
	pbkdf2Params
	hash func() hash.Hash
}

func pbkdf2Of(alg pkix.AlgorithmIdentifier) (*pbkdf2Config, error) {
	if !alg.Algorithm.Equal(oidPBKDF2) {
		return nil, fmt.Errorf("%w: funkcja wyprowadzania klucza %v", ErrUnsupportedAlgorithm, alg.Algorithm)
	}
	var c pbkdf2Config
	if _, err := asn1.Unmarshal(alg.Parameters.FullBytes, &c.pbkdf2Params); err != nil {
		return nil, fmt.Errorf("%w: PBKDF2: %v", ErrMalformed, err)
	}
	if c.Iterations < 1 || c.Iterations > maxIterations {
		return nil, fmt.Errorf("%w: PBKDF2 z %d iteracjami", ErrMalformed, c.Iterations)
	}
	c.hash = sha1.New // RFC 8018 § A.2: domyślnie hmacWithSHA1
	if c.PRF.Algorithm != nil {
		if c.hash = hmacHash(c.PRF.Algorithm); c.hash == nil {
			return nil, fmt.Errorf("%w: PBKDF2 z PRF %v", ErrUnsupportedAlgorithm, c.PRF.Algorithm)
		}
	}
	return &c, nil
}

// decrypt odszyfrowuje dane algorytmem PBES2 albo PBE PKCS#12 z 3DES i usuwa dopełnienie.
func decrypt(alg pkix.AlgorithmIdentifier, data []byte, password string, bmpPassword []byte) ([]byte, error) {
	var block cipher.Block
	var iv []byte
	switch {
	case alg.Algorithm.Equal(oidPBES2):
		var params pbes2Params
		if _, err := asn1.Unmarshal(alg.Parameters.FullBytes, &params); err != nil {
			return nil, fmt.Errorf("%w: PBES2: %v", ErrMalformed, err)
		}
		kdf, err := pbkdf2Of(params.KDF)
		if err != nil {
			return nil, err
		}
		size := 0
		switch scheme := params.Scheme.Algorithm; {
		case scheme.Equal(oidAES128CBC):
			size = 16
		case scheme.Equal(oidAES192CBC):
			size = 24
		case scheme.Equal(oidAES256CBC):
			size = 32
		case scheme.Equal(oidDESEDE3CBC):
			size = 24
		default:
			return nil, fmt.Errorf("%w: szyfr PBES2 %v; %s", ErrUnsupportedAlgorithm, scheme, legacyConversionHints)
		}
		if kdf.KeyLength != 0 && kdf.KeyLength != size {
			return nil, fmt.Errorf("%w: PBKDF2 z długością klucza %d", ErrMalformed, kdf.KeyLength)
		}
		if _, err := asn1.Unmarshal(params.Scheme.Parameters.FullBytes, &iv); err != nil {
			return nil, fmt.Errorf("%w: wektor IV: %v", ErrMalformed, err)
		}
		key, err := pbkdf2.Key(kdf.hash, password, kdf.Salt, kdf.Iterations, size)
		if err != nil {
			return nil, err
		}
		if params.Scheme.Algorithm.Equal(oidDESEDE3CBC) {
			block, err = des.NewTripleDESCipher(key)
		} else {
			block, err = aes.NewCipher(key)
		}
		if err != nil {
			return nil, err
		}
	case alg.Algorithm.Equal(oidPBEWithSHA3KeyDES), alg.Algorithm.Equal(oidPBEWithSHA2KeyDES):
		var params pbeParams
		if _, err := asn1.Unmarshal(alg.Parameters.FullBytes, &params); err != nil {
			return nil, fmt.Errorf("%w: parametry PBE: %v", ErrMalformed, err)
		}
		if params.Iterations < 1 || params.Iterations > maxIterations {
			return nil, fmt.Errorf("%w: PBE z %d iteracjami", ErrMalformed, params.Iterations)
		}
		size := 24
		if alg.Algorithm.Equal(oidPBEWithSHA2KeyDES) {
			size = 16
		}
		key := pkcs12KDF(sha1.New, 20, 64, params.Salt, bmpPassword, params.Iterations, 1, size)
		if size == 16 { // klucz dwuczęściowy: K1 K2 K1
			key = append(key, key[:8]...)
		}
		iv = pkcs12KDF(sha1.New, 20, 64, params.Salt, bmpPassword, params.Iterations, 2, 8)
		var err error
		if block, err = des.NewTripleDESCipher(key); err != nil {
			return nil, err
		}
	case len(alg.Algorithm) == len(oidPBEWithSHALegacy)+1 && alg.Algorithm[:len(oidPBEWithSHALegacy)].Equal(oidPBEWithSHALegacy):
		return nil, fmt.Errorf("%w: szyfr RC2 albo RC4 (pbeWithSHAAnd…RC2/RC4, %v), przestarzały i bez obsługi także w OpenSSL 3 bez -legacy; %s",
			ErrUnsupportedAlgorithm, alg.Algorithm, legacyConversionHints)
	default:
		return nil, fmt.Errorf("%w: szyfr %v", ErrUnsupportedAlgorithm, alg.Algorithm)
	}
	if len(iv) != block.BlockSize() || len(data) == 0 || len(data)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("%w: długość szyfrogramu albo wektora IV", ErrMalformed)
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	// dopełnienie PKCS#7 (RFC 8018 § 6.1.1); złe dopełnienie to zwykle złe hasło
	n := int(out[len(out)-1])
	if n == 0 || n > block.BlockSize() {
		return nil, ErrPassword
	}
	for _, b := range out[len(out)-n:] {
		if int(b) != n {
			return nil, ErrPassword
		}
	}
	return out[:len(out)-n], nil
}

// pkcs12KDF wyprowadza n bajtów klucza (id 1), wektora IV (id 2) albo klucza MAC (id 3) według
// RFC 7292 dodatek B.2; u to rozmiar skrótu, v rozmiar bloku funkcji skrótu.
func pkcs12KDF(newHash func() hash.Hash, u, v int, salt, password []byte, iterations int, id byte, n int) []byte {
	fill := func(src []byte) []byte {
		if len(src) == 0 {
			return nil
		}
		out := make([]byte, v*((len(src)+v-1)/v))
		for i := range out {
			out[i] = src[i%len(src)]
		}
		return out
	}
	d := bytes.Repeat([]byte{id}, v)
	i := append(fill(salt), fill(password)...)
	var out []byte
	for len(out) < n {
		h := newHash()
		h.Write(d)
		h.Write(i)
		a := h.Sum(nil)
		for r := 1; r < iterations; r++ {
			h.Reset()
			h.Write(a)
			a = h.Sum(a[:0])
		}
		out = append(out, a...)
		if len(out) >= n {
			break
		}
		b := make([]byte, v)
		for k := range b {
			b[k] = a[k%u]
		}
		// I_j = (I_j + B + 1) mod 2^(8v) dla każdego bloku v bajtów
		for j := 0; j < len(i); j += v {
			carry := 1
			for k := v - 1; k >= 0; k-- {
				sum := int(i[j+k]) + int(b[k]) + carry
				i[j+k], carry = byte(sum), sum>>8
			}
		}
	}
	return out[:n]
}

// bmpString koduje hasło dla KDF PKCS#12: UTF-16BE z dwoma bajtami zerowymi na końcu (RFC 7292 B.1).
func bmpString(s string) []byte {
	out := make([]byte, 0, 2*len(s)+2)
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return append(out, 0, 0)
}

func digestHash(oid asn1.ObjectIdentifier) (func() hash.Hash, int, int) {
	switch {
	case oid.Equal(oidSHA1):
		return sha1.New, 20, 64
	case oid.Equal(oidSHA256):
		return sha256.New, 32, 64
	case oid.Equal(oidSHA384):
		return sha512.New384, 48, 128
	case oid.Equal(oidSHA512):
		return sha512.New, 64, 128
	}
	return nil, 0, 0
}

func hmacHash(oid asn1.ObjectIdentifier) func() hash.Hash {
	switch {
	case oid.Equal(oidHMACWithSHA1):
		return sha1.New
	case oid.Equal(oidHMACWithSHA256):
		return sha256.New
	case oid.Equal(oidHMACWithSHA384):
		return sha512.New384
	case oid.Equal(oidHMACWithSHA512):
		return sha512.New
	}
	return nil
}

// explicitInner rozpakowuje znacznik [0] EXPLICIT: zwraca jedyny element w środku.
func explicitInner(v asn1.RawValue) (asn1.RawValue, error) {
	var inner asn1.RawValue
	if v.Class != asn1.ClassContextSpecific || v.Tag != 0 || !v.IsCompound {
		return inner, fmt.Errorf("%w: oczekiwany znacznik [0]", ErrMalformed)
	}
	if rest, err := asn1.Unmarshal(v.Bytes, &inner); err != nil || len(rest) != 0 {
		return inner, fmt.Errorf("%w: znacznik [0]: %v", ErrMalformed, errOr(err, "więcej niż jeden element"))
	}
	return inner, nil
}

// explicitOctets zwraca treść OCTET STRING w znaczniku [0] EXPLICIT (ContentInfo z id-data).
func explicitOctets(v asn1.RawValue) ([]byte, error) {
	inner, err := explicitInner(v)
	if err != nil {
		return nil, err
	}
	if inner.Class != asn1.ClassUniversal || inner.Tag != asn1.TagOctetString {
		return nil, fmt.Errorf("%w: oczekiwany OCTET STRING", ErrMalformed)
	}
	return octets(inner)
}

// octets zwraca treść ciągu OCTET STRING, także w postaci złożonej (BER: kawałki jeden po drugim).
func octets(v asn1.RawValue) ([]byte, error) {
	if !v.IsCompound {
		return v.Bytes, nil
	}
	var out []byte
	for rest := v.Bytes; len(rest) > 0; {
		var part asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &part); err != nil {
			return nil, fmt.Errorf("%w: OCTET STRING: %v", ErrMalformed, err)
		}
		chunk, err := octets(part)
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}

func localKeyID(attrs []bagAttribute) []byte {
	for _, a := range attrs {
		if a.ID.Equal(oidLocalKeyID) {
			var id []byte
			if _, err := asn1.Unmarshal(a.Values.Bytes, &id); err == nil {
				return id
			}
		}
	}
	return nil
}

func errOr(err error, text string) any {
	if err != nil {
		return err
	}
	return errors.New(text)
}
