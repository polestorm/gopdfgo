# ADR 0026: PDF/A-3b i załączniki dokumentu

Status: przyjęta 2026-11-20. Rozszerza ADR 0016 (PDF/A-2b).

## Kontekst

E-faktury w rodzaju Factur-X i ZUGFeRD to pliki PDF/A-3 z osadzonym XML-em faktury jako plikiem
stowarzyszonym z dokumentem. ISO 19005-2 § 6.8 dopuszcza w PDF/A-2 tylko załączniki, które same są plikami
PDF/A-1 albo PDF/A-2. ISO 19005-3 § 6.8 dopuszcza pliki dowolnego typu, ale wymaga kilku rzeczy:
- każdy osadzony plik ma być stowarzyszony z dokumentem albo jego częścią kluczem `AF`;
- specyfikacja pliku ma `/F`, `/UF` i `/AFRelationship`;
- strumień pliku ma typ MIME w `/Subtype`, a gdy typu nie znamy, `application/octet-stream`.

Silnik miał po WeasyPrint załączniki z HTML: `<link rel=attachment>` jako plik całego dokumentu
i `<a rel=attachment>` jako adnotację pliku. Nie zapisywał jednak typu MIME ani związku z dokumentem.
Wygląd adnotacji był słownikiem zamiast strumienia (ISO 19005-3 § 6.3.3), a niepobrany plik adnotacji
kończył renderowanie paniką.

## Decyzja

1. `Options.Attachments []Attachment` to pliki całego dokumentu. Każdy ma pola `Name`, `Data`, `MediaType`,
   `Description`, `Relationship` i `ModTime`. Związek przyjmuje jedną z wartości ISO 32000-2 tabela 43:
   `Source`, `Data`, `Alternative`, `Supplement`, `Unspecified`; pusty oznacza `Unspecified`. Nazwa jest
   wymagana i niepowtarzalna, a nieznany związek kończy `Render` błędem `ErrInvalidAttachment`.
2. Pliki z `Options.Attachments` i z `<link rel=attachment>` trafiają do drzewa `/EmbeddedFiles` katalogu:
   - kluczem jest nazwa pliku, drzewo jest posortowane, a korzeń nie ma `/Limits` (ISO 32000-1 § 7.9.6);
   - specyfikacja pliku ma `/F`, `/UF`, `/Desc` i `/AFRelationship` w każdym trybie;
   - strumień pliku ma `/Subtype` z typem MIME (domyślnie `application/octet-stream`), sumę kontrolną
     i rozmiar.
3. `ModTime` trafia do `/Params /ModDate` tylko wtedy, gdy nie jest zerowy. ISO 32000-1 tabela 46 czyni ten
   wpis opcjonalnym, a ISO 19005-3 § 6.8 go nie wymaga. Silnik nie wstawia bieżącego czasu, więc te same
   wejścia dają te same bajty.
4. `Conformance = PDFA3b` (wiersz poleceń `-pdfa 3b`) daje wszystko, co PDF/A-2b, a ponadto XMP
   z `pdfaid:part=3`. Katalog dostaje tablicę `/AF` ze wszystkimi osadzonymi plikami, także z adnotacji
   `<a rel=attachment>`. Norma pozwala stowarzyszyć plik z całym dokumentem, a czytniki e-faktur szukają go
   w katalogu.
5. Załączniki z HTML są pobierane przez `Options.Resolver`, jak inne zasoby, bez dekodowania tekstu:
   - typ MIME pochodzi z odpowiedzi Resolvera albo z rozszerzenia adresu;
   - związek to `Unspecified`, bo HTML go nie określa, a tabela 43 przewiduje tę wartość dla związku nieznanego;
   - atrybut `title` elementu `<link>` staje się opisem pliku, jak w WeasyPrint.
6. W trybie PDF/A-2b załącznik, który nie jest plikiem PDF/A-1 ani PDF/A-2, kończy `Render` błędem
   `ErrAttachmentNotPDFA` zamiast dawać plik niezgodny z § 6.8. Za plik PDF/A uznajemy plik, który zaczyna
   się od `%PDF-` i deklaruje w metadanych XMP `pdfaid:part` 1 albo 2. Metadane PDF/A nie mogą być
   filtrowane (ISO 19005-1 § 6.7.2, ISO 19005-2 § 6.6.2.1), więc deklarację widać w surowych bajtach.
   To sprawdzenie deklaracji, nie walidacja: za zgodność samego załącznika odpowiada wywołujący.
7. Adnotacja pliku ma wygląd zapisany jako pusty strumień (§ 6.3.3). Adnotacja, której pliku nie udało się
   pobrać, jest pomijana z ostrzeżeniem.
8. Wiersz poleceń: `-attach plik[,związek[,typ MIME]]`; flaga może się powtarzać.

Zgodność sprawdza `pdfa3_test.go` walidatorem veraPDF w profilach 3b i 2b; bez walidatora ta część jest
pomijana.

## Skutki

- Pomiar 2026-11-20, veraPDF z /opt/homebrew/bin:
  - korpus 22 dokumentów w trybie 2b: 22 z 22 zgodnych;
  - dokumenty 01, 03, 07 i 15 z załącznikami XML (Alternative) i TXT (Supplement) w trybie 3b: zgodne;
  - dokument z dwoma `<link rel=attachment>` i jednym `<a rel=attachment>` w trybie 3b: zgodny.
- Zwykły PDF bez załączników ma te same bajty co przed zmianą (22 z 22 dokumentów korpusu).
- Typ MIME odgadywany z rozszerzenia (`mime.TypeByExtension`) zależy od tabel systemu. Dla załączników
  z HTML bez typu od Resolvera może się więc różnić między maszynami. `Options.Attachments` niczego nie zgaduje.
- Poza zakresem:
  - schemat rozszerzenia XMP Factur-X/ZUGFeRD (`fx:`), który profile e-faktur wymagają obok `pdfaid`;
  - PDF/A-3a i PDF/A-3u;
  - stowarzyszanie plików z częściami dokumentu (stroną, adnotacją) zamiast z całym dokumentem.
