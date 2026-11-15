# ADR 0006: dolny margines dziecka przy `min-height` i `max-height` jak w Chromium, z przełącznikiem

Status: przyjęta 2026-11-15 (decyzja użytkownika). Rozstrzyga pytanie 18 z `open-questions.md`.

## Kontekst

Gdy `min-height` albo `max-height` zmienia wysokość pudełka o `height: auto`, są dwa zachowania dolnego
marginesu ostatniego dziecka:

- dosłowny tekst CSS 2.2 § 10.7 („nie wpływają na zwijanie marginesów”): margines zwija się z marginesem
  rodzica, wychodzi poza pudełko i odsuwa następne; tak oczekuje test WPT `margin-collapse-038` (2010);
- przeglądarki, w tym Chromium, oraz cztery nowsze testy WPT (`margin-collapse-min-height-001/002`,
  `min-height-separates-margin`, `max-height-separates-margin`): margines zostaje w pudełku.

Norma i testy są tu sprzeczne. Dokumenty do druku powstają zwykle pod podglądem w przeglądarce, a narzędzia
HTML → PDF, które silnik ma zastępować, najczęściej opierają się na Chromium.

## Decyzja

1. Domyślnie silnik zachowuje się jak Chromium: margines zostaje w pudełku.
2. Zachowanie z dosłownego tekstu normy włącza jeden parametr: `Options.MarginsEscapeHeightLimits`
   (w CLI flaga `-margins-escape-height-limits`).
3. Przełączniki tego rodzaju trafiają w silniku do `tree.Behavior` (pole `tree.HTML.Behavior`, w układzie
   `layoutContext.behavior`), bez zmiennych globalnych; kolejne rozbieżności norma–przeglądarki dostają tam
   własne pola.

## Skutki

Sondaż ekranowy w trybie domyślnym zalicza cztery nowsze testy i nie zalicza `margin-collapse-038`;
z włączonym parametrem odwrotnie.

Kod: `internal/webrender/html/layout/block.go` (warunek przy ograniczaniu wysokości w `blockContainerLayout`),
`render.go`, test `TestMarginsEscapeHeightLimits`.
