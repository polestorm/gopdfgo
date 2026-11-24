// Polecenie gopdfgo-sign podpisuje gotowy plik PDF podpisem PAdES (pakiet sign, ADR 0050, 0051).
//
//	gopdfgo-sign -in dokument.pdf -out podpisany.pdf -p12 podpis.p12 -password-env HASLO_P12
//	gopdfgo-sign -in dokument.pdf -out podpisany.pdf -key klucz.pem -cert certyfikat.pem [-chain łańcuch.pem]
//
// Klucz pochodzi z pliku wskazanego jawnie: PKCS#12 (-p12, hasło ze zmiennej środowiska -password-env
// albo z pierwszego wiersza pliku -password-file; hasła nie podaje się w wierszu poleceń, bo widać je
// na liście procesów) albo PEM bez szyfrowania (-key: PKCS#8, PKCS#1, SEC1; -cert i -chain: certyfikaty).
//
// Poziom -level: B-B (domyślny, bez połączeń sieciowych), B-T (znacznik czasu od -tsa), B-LT (B-T
// i dane o ważności certyfikatów: OCSP i CRL z adresów zapisanych w certyfikatach). Połączenia
// sieciowe są możliwe tylko z podanym -tsa; B-T i B-LT bez -tsa kończą się błędem.
//
// -field wskazuje istniejące puste pole podpisu (np. z atrybutu data-pdf-signature w HTML); bez niego
// podpis trafia do nowego niewidocznego pola. Polecenie jest osobnym programem, żeby zwalidowana
// binarka gopdfgo nie zawierała kodu podpisu (ADR 0049 pkt 4).
package main

import (
	"context"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polestorm/gopdfgo/sign"
	"github.com/polestorm/gopdfgo/sign/keyfile"
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "gopdfgo-sign:", err)
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// usageError to błąd w argumentach (kod wyjścia 2).
type usageError struct{ error }

func usagef(format string, args ...any) error { return usageError{fmt.Errorf(format, args...)} }

// run wykonuje polecenie z argumentami args; getenv czyta zmienne środowiska (w testach podstawiane).
func run(args []string, getenv func(string) string, stderr io.Writer) error {
	flags := flag.NewFlagSet("gopdfgo-sign", flag.ContinueOnError)
	flags.SetOutput(stderr)
	in := flags.String("in", "", "PDF file to sign")
	out := flags.String("out", "", "output file (signed PDF)")
	p12 := flags.String("p12", "", "PKCS#12 file (.p12, .pfx) with the key and certificates")
	passwordEnv := flags.String("password-env", "", "name of the environment variable holding the -p12 password")
	passwordFile := flags.String("password-file", "", "file holding the -p12 password (first line)")
	keyFile := flags.String("key", "", "unencrypted PEM private key (PKCS#8, PKCS#1, SEC1)")
	certFile := flags.String("cert", "", "PEM certificate of the -key key")
	chainFile := flags.String("chain", "", "PEM intermediate certificates (with -p12 or -key)")
	level := flags.String("level", "B-B", "PAdES level: B-B, B-T, B-LT")
	tsa := flags.String("tsa", "", "RFC 3161 timestamp server URL (required for B-T and B-LT; the only source of network connections)")
	reason := flags.String("reason", "", "reason for signing (/Reason)")
	location := flags.String("location", "", "place of signing (/Location)")
	name := flags.String("name", "", "signer's name (/Name)")
	contact := flags.String("contact", "", "signer's contact (/ContactInfo)")
	field := flags.String("field", "", "name of an existing empty signature field (e.g. from data-pdf-signature); default: a new invisible field")
	timeout := flags.Duration("timeout", time.Minute, "time limit for the whole signature, connections included")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	if flags.NArg() != 0 {
		return usagef("unexpected arguments: %q", flags.Args())
	}
	if *in == "" || *out == "" {
		return usagef("-in and -out are required")
	}

	opts := sign.Options{Reason: *reason, Location: *location, Name: *name, ContactInfo: *contact, FieldName: *field}
	switch strings.ToUpper(*level) {
	case "B-B", "BB":
		opts.Level = sign.LevelBB
	case "B-T", "BT":
		opts.Level = sign.LevelBT
	case "B-LT", "BLT":
		opts.Level = sign.LevelBLT
	default:
		return usagef("unknown -level %q (allowed: B-B, B-T, B-LT)", *level)
	}
	if opts.Level >= sign.LevelBT && *tsa == "" {
		return usagef("level %s requires -tsa (a timestamp server URL)", opts.Level)
	}

	var id *keyfile.Identity
	switch {
	case *p12 != "" && (*keyFile != "" || *certFile != ""):
		return usagef("-p12 cannot be combined with -key and -cert")
	case *p12 != "":
		if *passwordEnv != "" && *passwordFile != "" {
			return usagef("give -password-env or -password-file, not both")
		}
		password, err := readPassword(*passwordEnv, *passwordFile, getenv)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(*p12)
		if err != nil {
			return err
		}
		if id, err = keyfile.ParsePKCS12(data, password); err != nil {
			if errors.Is(err, keyfile.ErrPassword) && *passwordEnv == "" && *passwordFile == "" {
				return fmt.Errorf("%w (give the password with -password-env or -password-file)", err)
			}
			return fmt.Errorf("%s: %w", *p12, err)
		}
	case *keyFile != "" && *certFile != "":
		if *passwordEnv != "" || *passwordFile != "" {
			return usagef("a password applies only to -p12 (encrypted PEM keys are not supported)")
		}
		key, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		cert, err := os.ReadFile(*certFile)
		if err != nil {
			return err
		}
		if id, err = keyfile.ParsePEM(key, cert, nil); err != nil {
			return err
		}
	default:
		return usagef("-p12, or -key with -cert, is required")
	}
	if *chainFile != "" {
		data, err := os.ReadFile(*chainFile)
		if err != nil {
			return err
		}
		extra, err := keyfile.ParsePEMCertificates(data)
		if err != nil {
			return fmt.Errorf("%s: %w", *chainFile, err)
		}
		id.Chain = appendNew(id.Chain, extra)
	}
	opts.Signer, opts.Certificate, opts.Chain = id.Signer, id.Certificate, id.Chain

	if *tsa != "" {
		client := &http.Client{Timeout: *timeout}
		opts.TSA = sign.TSAClient{URL: *tsa, HTTPClient: client}
		if opts.Level == sign.LevelBLT {
			opts.Revocation = sign.RevocationClient{HTTPClient: client}
		}
	}

	pdf, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	signed, err := sign.SignPDF(ctx, pdf, opts)
	if err != nil {
		return err
	}
	return writeFile(*out, signed)
}

// readPassword czyta hasło ze zmiennej środowiska albo z pierwszego wiersza pliku; bez nich hasło jest
// puste (plik PKCS#12 bez hasła).
func readPassword(env, file string, getenv func(string) string) (string, error) {
	switch {
	case env != "":
		value := getenv(env)
		if value == "" {
			return "", usagef("environment variable %s is empty or not set", env)
		}
		return value, nil
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		line, _, _ := strings.Cut(string(data), "\n")
		return strings.TrimSuffix(line, "\r"), nil
	}
	return "", nil
}

// appendNew dopisuje certyfikaty, których jeszcze nie ma na liście.
func appendNew(list, extra []*x509.Certificate) []*x509.Certificate {
	for _, cert := range extra {
		known := false
		for _, c := range list {
			known = known || c.Equal(cert)
		}
		if !known {
			list = append(list, cert)
		}
	}
	return list
}

// writeFile zapisuje wynik przez plik tymczasowy w tym samym katalogu, żeby przerwany zapis nie zostawił
// uciętego pliku (także gdy -out wskazuje plik wejściowy).
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gopdfgo-sign-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
