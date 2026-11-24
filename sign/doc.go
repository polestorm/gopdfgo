// Package sign signs finished PDF files with a PAdES digital signature (ETSI EN 319 142-1).
//
// The package is optional (ADR 0049, 0050): the gopdfgo core does not import it, so a program that
// does not import it has no signing code in its binary. The CMS signature (RFC 5652) is detached, SHA-256,
// with the signed attributes content-type, message-digest and signing-certificate-v2, in an invisible
// signature field on the first page or in an existing empty field named by Options.FieldName.
// The signature is appended as an incremental update, so it works on any valid, unencrypted PDF
// (also tagged, PDF/A-2, PDF/A-3, Factur-X and already signed) and does not break earlier signatures.
// The package does not read keys from files: the caller supplies the key and the certificate; reading PKCS#12
// and PEM files is provided by the separate package sign/keyfile (used by the gopdfgo-sign command, ADR 0051).
//
// Visible field: an HTML element with the attribute data-pdf-signature="Name" gets, at rendering time, an empty
// signature field on the rectangle of its box (the gopdfgo core, without any code of this package; in a tagged
// PDF the widget has a Form structure element). Signing in that field:
//
//	opts.FieldName = "Name"
//	signed, err := sign.SignPDF(ctx, result.PDF, opts)
//
// The field gets /V; the widget, the page and the field's appearance stay unchanged: what shows at the signature's
// place is the element's HTML content. An unknown name gives ErrUnknownField, a signed field ErrFieldNotEmpty.
//
// Levels (Options.Level):
//
//   - LevelBB (the default): the signature alone; no network connections.
//   - LevelBT: the signature and an RFC 3161 timestamp of the signature value (the unsigned attribute
//     signature-time-stamp-token) from Options.TSA.
//   - LevelBLT: like LevelBT, plus a second incremental update with the DSS dictionary
//     (ISO 32000-2 § 12.8.4.3): certificates, OCSP responses and CRLs for the signing
//     certificate, the TSA certificate and their issuers, with the signature's /VRI entry.
//
// The package connects to the network only through clients passed explicitly: Options.TSA (TimestampAuthority,
// e.g. a TSAClient with an address and an *http.Client) and Options.Revocation (RevocationFetcher, e.g.
// a RevocationClient, which takes the OCSP and CRL addresses from the certificates). The responses are checked
// by the package itself (status, digest, nonce, TSA and responder signatures, certificate status), so a custom
// implementation of these interfaces handles transport only. ctx aborts connections and signing (ContextSigner).
//
// PAdES B-B signature:
//
//	result, err := htmlpdf.Render(ctx, html, htmlpdf.Options{Fonts: fonts, Conformance: htmlpdf.PDFA2b})
//	if err != nil {
//		return err
//	}
//	// key: *rsa.PrivateKey or *ecdsa.PrivateKey (or any crypto.Signer, e.g. an HSM)
//	// cert: the *x509.Certificate matching the key; chain: intermediate certificates
//	signed, err := sign.SignPDF(ctx, result.PDF, sign.Options{
//		Signer:      key,
//		Certificate: cert,
//		Chain:       chain,
//		Reason:      "Document approval",
//		Location:    "Warsaw",
//	})
//
// PAdES B-T (timestamp):
//
//	client := &http.Client{Timeout: 30 * time.Second}
//	signed, err := sign.SignPDF(ctx, result.PDF, sign.Options{
//		Signer:      key,
//		Certificate: cert,
//		Chain:       chain,
//		Level:       sign.LevelBT,
//		TSA:         sign.TSAClient{URL: "https://tsa.example/rfc3161", HTTPClient: client},
//	})
//
// PAdES B-LT with validation data fetched from the network (OCSP from authorityInfoAccess, CRL
// from cRLDistributionPoints). Chain should reach the root, and the issuer certificates of the TSA
// certificate are supplied in ValidationData.Certificates:
//
//	signed, err := sign.SignPDF(ctx, result.PDF, sign.Options{
//		Signer:         key,
//		Certificate:    cert,
//		Chain:          []*x509.Certificate{intermediate, root},
//		Level:          sign.LevelBLT,
//		TSA:            sign.TSAClient{URL: "https://tsa.example/rfc3161", HTTPClient: client},
//		Revocation:     sign.RevocationClient{HTTPClient: client},
//		ValidationData: sign.ValidationData{Certificates: []*x509.Certificate{tsaCA}},
//	})
//
// PAdES B-LT offline: OCSP responses and CRLs fetched earlier (DER) instead of Revocation;
// missing data for any of the certificates gives ErrNoValidationData:
//
//	opts.Revocation = nil
//	opts.ValidationData = sign.ValidationData{
//		Certificates:  []*x509.Certificate{tsaCA},
//		OCSPResponses: [][]byte{signerOCSP},
//		CRLs:          [][]byte{caCRL},
//	}
//
// An external signer (HSM, smart card, remote qualified signature) implements crypto.Signer:
// Public returns the public key from the certificate, and Sign(rand, digest, crypto.SHA256) receives the
// SHA-256 digest of the signed attributes and returns an RSA PKCS#1 v1.5 signature or an ECDSA one in DER.
// A signer that also implements ContextSigner receives ctx in SignContext. SignPDF verifies the signature
// with the key from the certificate before writing it.
//
// At level B-LT the last update of the file (DSS) lies outside the signature's /ByteRange, as
// ISO 32000-2 § 12.8.4.3 provides; PAdES validators treat it as permitted, while pdfsig (Poppler)
// shows "Not total document signed" next to the signature.
package sign
