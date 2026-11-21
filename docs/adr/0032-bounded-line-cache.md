# ADR 0032: Ograniczony rozmiar pamięci złamanych wierszy

Status: przyjęta 2026-11-21.

## Kontekst

Silnik tekstu pamięta wynik łamania pierwszego wiersza (`WrapWordBreak`) pod kluczem: pozostały tekst
akapitu, styl, szerokość, zgoda na łamanie w wyrazie. Źródło trzymało te wyniki w mapie bez limitu przez
całe renderowanie. Klucz to cały pozostały tekst, więc akapit złamany na n wierszy kosztował około n²/2
znaków samych kluczy, a do tego glify każdego wiersza. Po rysowaniu stron w trakcie układu (ADR 0027)
była to największa pojedyncza pozycja żywej pamięci, rosnąca liniowo z liczbą stron: ok. 70 MB przy
książce 256 stron i ok. 400 MB przy 1021 stronach.

Pomiar trafień (2026-11-21, instrumentacja tymczasowa, książka 1021 stron z innym tekstem w każdej
kopii): trafienia dotyczą głównie wpisów świeżych (ponowne próby podziału strony, szerokości
min/max-content); trafienia w wpisy sprzed wielu stron pochodzą z treści powtórzonej w dokumencie.

## Decyzja

1. Pamięć ma dwa pokolenia (`text/layout_cache.go`). Wpisy trafiają do bieżącego; gdy jego szacowany
   rozmiar (tekst klucza, glify w zapisie zwartym, stały narzut) przekroczy 16 MB, bieżące staje się
   poprzednim, a dawne poprzednie jest porzucane. Trafienie w poprzednim przenosi wpis do bieżącego.
2. Mapy nie są przeglądane, a `WrapWordBreak` jest funkcją swoich argumentów, więc wynik nie zależy od
   trafień: porównanie bajtowe korpusu W3C (11 689 dokumentów, 46 756 renderowań) dało 0 różnic.
3. Limit jest stały, bez opcji. Dokument powtarzający całe akapity wiele razy na przestrzeni setek stron
   traci na czasie (książka ×4, ta sama treść 4 razy: 4,8 → 6,4 s); przy treści niepowtarzalnej czas jest
   ten sam, a pamięć mniejsza (1,30–1,54 → 0,73 GB przy 1021 stronach).

## Konsekwencje

- Pamięć szczytowa przy dużych dokumentach zależy głównie od wejścia (style, drzewo pudełek, obrazy).
- Gdyby powtarzalne dokumenty (np. setki identycznych formularzy) okazały się ważne, można dodać pamięć
  krótkich tekstów bez limitu albo opcję rozmiaru; na razie bez tego.
