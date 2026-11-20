# ADR 0022: podział po dywizie przed cyfrą jak w Chromium

Status: przyjęta 2026-11-20 (decyzja użytkownika).

## Kontekst

W sprawozdaniu e-SF Chrome przeniósł część kodu pocztowego „03-228” do następnego wiersza, czyli podzielił
go po dywizie. gopdfgo trzymał cały kod w jednym wierszu. W fakturach i raportach taki układ znaków jest
częsty: kody pocztowe, numery telefonów, zakresy lat, numery dokumentów.

Norma UAX #14 (Unicode 18, rewizja 57, 2026-09-01) mówi w algorytmie domyślnym:

- LB21 „× HY”: nie łamie się przed dywizem-minusem (U+002D);
- LB25 „HY × NU”: nie łamie się między dywizem-minusem a cyfrą, bo ten znak bywa minusem („-5”).

W „03-228” algorytm domyślny nie ma więc żadnego miejsca podziału. Ta sama norma zaznacza jednak, że
odróżnienie dywizu od minusa wymaga analizy kontekstu. Przy LB12a dodaje, że łamanie po dywizie odpowiada
powszechnej praktyce implementacji. CSS Text 3 § 5.1 pozwala przeglądarce dostosować reguły UAX #14.

Chrome 157 (pomiar 2026-11-20, 14 przypadków w kolumnie o szerokości zero) łamie po dywizie przed cyfrą,
gdy przed dywizem stoi litera albo cyfra: „03-|228”, „555-|1234”, „2020-|2021”, „a-|1”, „1.5-|2.5”.
Nie łamie, gdy dywiz jest minusem na początku („-5”) ani gdy stoi po nawiasie („(1)-2”). Dywizy przed
literami („abc-|def”, „1-|a”) oba silniki łamały już wcześniej tak samo.

## Decyzja

1. Norma zostawia tu wybór, więc według zasady projektu zachowujemy się jak Chromium: dywiz-minus po
   literze albo cyfrze dziesiętnej i przed cyfrą dziesiętną jest miejscem podziału.
2. Wykonanie: przy wyznaczaniu miejsc podziału (`hyphensBeforeDigits` w
   `internal/webrender/text/engine_gotext.go`) taki znak jest traktowany jak U+2010 HYPHEN (klasa HH).
   Po nim UAX #14 pozwala łamać, a LB21 dalej zabrania łamania przed nim. Kształty, szerokości i tekst
   w PDF się nie zmieniają.
3. Bez przełącznika: to zachowanie wszystkich przeglądarek opartych na ICU i nie przeczy normie.

## Skutki

- We wszystkich 14 przypadkach podział jest taki sam jak w Chrome 157 (test `TestHyphenBeforeDigitsBreaks`).
- Sondaż W3C i reftesty druku bez zmian w zaliczonych (pomiar w `THIRD_PARTY.md`).
- Szerokość minimalna (min-content) tekstu z takim dywizem maleje: „03-228” zajmuje w kolumnie tyle, co „228”.
