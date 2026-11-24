# Wizja

Stan na 2026-11-13. Zastępuje wersję z 2026-11-12, która wyprowadzała projekt z potrzeb jednego odbiorcy.

## Czym jest

Silnik ogólnego użytku w czystym Go, który ze **statycznego HTML + CSS + SVG** tworzy dokument stronicowany
i zapisuje go jako PDF. Jest biblioteką (`htmlpdf.Render`), którą program w Go dołącza do własnej binarki zamiast
uruchamiać przeglądarkę.

Silnik jest **dokumentowy**, nie przeglądarkowy: implementuje CSS 2.1, wybrane moduły CSS 3 (flex, zmienne, `calc`,
kolumny, transformacje 2D) i CSS Paged Media. Nie zna JavaScriptu, zdarzeń, animacji, formularzy interaktywnych ani
sieci. Wzorcami tej klasy są WeasyPrint i Prince.

## Cele

1. **Zastąpić Chromium czymś o rząd wielkości mniejszym.** Chromium z bibliotekami to ok. 400 MB,
   `chrome-headless-shell` 150–200 MB (`landscape.md`). Cel: binarka z silnikiem co najmniej 10 razy mniejsza;
   stan na 2026-11-12 to ok. 27 MB przed optymalizacją (ADR 0002).
2. **Pełna kontrola nad kodem źródłowym.** Cały kod układu i zapisu PDF leży w repozytorium i jest nasz do
   czytania, poprawiania, upraszczania i przepisywania. Kod przejęty z innych projektów traktujemy tak samo
   (`docs/provenance.md`); zależności zewnętrzne ograniczamy do małych, stabilnych bibliotek.
3. **Determinizm renderowania.** Te same bajty wejścia, te same fonty i ta sama wersja silnika dają te same bajty
   PDF, na każdej maszynie i przy każdym uruchomieniu. Znaczniki czasu i identyfikatory ustawia wywołujący.
4. **Pokrycie w wybranych ramach, nie konkurencja z Chromium.** Silnik ma dobrze drukować do PDF typowe dokumenty:
   faktury, dokumenty tekstowe z osadzonymi obrazami, raporty z tabelami, grafikę wektorową, nagłówki i stopki
   stron. Kto potrzebuje stu procent CSS, używa Chromium.

## Wzorzec zgodności: normy, nie biblioteki

Punktem odniesienia są normy i specyfikacje: W3C dla HTML, CSS i SVG, ISO 32000 dla PDF, ISO 19005 dla PDF/A.
Gdy przejęty kod (WeasyPrint i jego port) albo inna biblioteka zachowuje się inaczej niż specyfikacja, poprawiamy
zachowanie i testy według specyfikacji; zgodność z normą jest ważniejsza niż kompatybilność z biblioteką źródłową.
Miarą są testy organizacji normalizacyjnych (web-platform-tests), nie testy odziedziczone po bibliotece.
Przeglądarka jest wzorcem tylko tam, gdzie specyfikacja zostawia swobodę.

## Jak rozstrzygamy spory

Gdy pełniejsze pokrycie kłóci się z innymi wartościami, wygrywają w tej kolejności: **rozmiar, przewidywalność,
szybkość, czystość kodu**. Świadomie poświęcamy przypadki brzegowe:

- funkcja rzadko używana w dokumentach, a droga w kodzie albo w rozmiarze binarki, może poczekać albo być
  włączana opcją; decyzję poprzedza pomiar, nie przeczucie;
- zachowanie proste i udokumentowane jest lepsze niż zgodne z przeglądarką co do piksela, ale trudne do
  przewidzenia;
- to, czego silnik nie obsługuje, zgłasza jako ostrzeżenie; nie zgaduje i nie pomija po cichu.

Ramy opisuje `scope.md`. Rozszerzamy je tam, gdzie zwiększa to użyteczność dla typowych dokumentów, a nie po to,
żeby odtworzyć wszystko, co robi przeglądarka albo silnik źródłowy. Z istniejących funkcji nie rezygnujemy na
zapas: gdy coś działa źle, oceniamy przydatność i koszt dokończenia i wybieramy między poprawą, pozostawieniem
a wyłączeniem do czasu poprawy (`feature-audit.md`).

## Zasady projektowe

- **Jawne luki.** Nieobsługiwana właściwość CSS lub element trafia do `Result.Warnings`.
- **Fonty i zasoby tylko od wywołującego.** Silnik nie czyta fontów systemowych, plików ani sieci; dostaje fonty
  w opcjach i zasoby przez `Resolver`, który domyślnie odmawia.
- **Czyste Go, zero cgo.** Kompilacja krzyżowa na linux/amd64 i linux/arm64, jedna binarka, brak bibliotek
  systemowych.
- **Zależności na licencjach permisywnych** (MIT, BSD, Apache-2.0; fonty OFL). Brak LGPL/GPL, bo linkujemy
  statycznie.
- **Odporność na dowolne wejście.** Błędny HTML, głębokie zagnieżdżenie czy nieznany CSS nie mogą skończyć się
  paniką ani zawieszeniem.
- **Testy przed opinią.** Każdą zmianę mierzymy na korpusie (`../testdata/README.md`): dokumenty, reftesty druku
  WPT ze stanem bazowym, testy równoważności, determinizm bajtów.
- **Lista wyświetlania jako granica** między układem a formatem wyjścia (ADR 0001); dziś istnieje jako interfejs
  `backend` przejętego silnika, własna lista jest w planie.

## Dla kogo

Każdy program w Go, który generuje dokumenty z własnych szablonów i nie chce wozić przeglądarki. Przyszli odbiorcy
wewnętrzni to QMS i xml.pro (ten drugi renderuje dziś przez Chromium w `gohtmlpdf`); są przykładami użycia,
nie źródłem wymagań.

## Czym nie jest

- Nie jest przeglądarką: brak JS, DOM API, zdarzeń, animacji, wideo, canvas, Web Components.
- Nie jest silnikiem „do wszystkiego”: dowolna strona z internetu nie jest celem.
- Nie jest edytorem ani podglądem interaktywnym.
- Nie jest biblioteką PDF ogólnego przeznaczenia (scalanie, wypełnianie formularzy, odczyt i weryfikacja
  podpisów); oddaje PDF, który inne narzędzia mogą dalej przetwarzać. Wyjątkiem są funkcje opcjonalne
  w osobnych pakietach, których rdzeń nie importuje (ADR 0049): podpis PAdES gotowego pliku (pakiet `sign`,
  polecenie `gopdfgo-sign`, ADR 0050 i 0051).
