# ADR 0001: lista wyświetlania jako warstwa niezależna od formatu wyjścia

Status: przyjęta 2026-11-12. Rozstrzyga pytanie 2 z `open-questions.md`.

## Kontekst

Rozważano, czy silnik „tylko do PDF”, rysujący wprost do pliku bez warstwy pośredniej, byłby prostszy. Ocena w
`../estimates.md` pkt 4: warstwa pośrednia to ok. 3% kodu silnika, jej brak oszczędza ok. 2% pracy (backendy SVG
i PNG), a kosztuje złote testy układu niezależne od bajtów PDF, podgląd wydruku jako SVG na stronę i wymienność
biblioteki PDF. Wszystkie rozważane silniki (WeasyPrint i jego port, go-webengine, biblioteka Delphi, tdewolff/canvas)
mają taką warstwę.

## Decyzja

1. Układ kończy się **listą wyświetlania**: strony × ciąg operacji (ścieżka, glify, obraz, przycięcie, przezroczystość,
   grupa z transformacją). Lista nie zna HTML ani formatu wyjścia.
2. Backendy (PDF, SVG na stronę, PNG na stronę) czytają wyłącznie listę. PDF jest pierwszym i wymaganym, SVG i PNG
   drugim i trzecim.
3. Lista ma postać tekstową do testów złotych i diffów.
4. Przy adopcji obcego silnika adapter kończy się na jego odpowiedniku tej warstwy (`backend.Document` w webrender,
   `paint.Canvas` w go-webengine), nie na gotowym PDF, o ile silnik na to pozwala.

## Konsekwencje

- Zwężenia zakresu szukamy po stronie wejścia (CSS, Paged Media), nie wyjścia.
- Zmiana biblioteki PDF (np. dla PDF/A) nie dotyka układu.
- Testy układu nie wymagają zewnętrznego rasteryzatora PDF.
