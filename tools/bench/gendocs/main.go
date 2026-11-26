// gendocs writes fictional business documents for the comparison with folio and Chromium (ADR 0055):
// four invoices in the layout of a Polish KSeF e-invoice printout and five financial statements in the
// layout of a Polish e-SF report. All names, numbers and identifiers are made up; the line item labels of
// the statements follow the annex to the Polish Accounting Act. The output is deterministic (fixed seeds).
//
//	go run ./tools/bench/gendocs testdata/comparison
//
// The documents stand in for real invoices and statements that cannot be published; they have the same
// kind of content (tables over several pages, repeated headers, page counters in margin boxes, grids,
// details elements, SVG icons and a QR code) but are not copies of them.
package main

import (
	"fmt"
	"html"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

const fontFaces = `@font-face { font-family: "Noto Sans"; src: url("../fonts/NotoSans-Regular.ttf") format("truetype"); font-weight: 400; }
@font-face { font-family: "Noto Sans"; src: url("../fonts/NotoSans-Bold.ttf") format("truetype"); font-weight: 700; }
@font-face { font-family: "Noto Sans"; src: url("../fonts/NotoSans-Italic.ttf") format("truetype"); font-weight: 400; font-style: italic; }
@font-face { font-family: "Noto Sans Mono"; src: url("../fonts/NotoSansMono-Regular.ttf") format("truetype"); }
`

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gendocs <output dir>")
		os.Exit(2)
	}
	out := os.Args[1]
	write := func(name, doc string) {
		if err := os.WriteFile(filepath.Join(out, name+".html"), []byte(doc), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(name, len(doc))
	}
	write("invoice-a-basic", invoice(1, 2, false))
	write("invoice-b-60rows", invoice(2, 60, false))
	write("invoice-c-rates-transfer", invoice(3, 5, true))
	write("invoice-d-140rows", invoice(4, 140, false))
	// liczba not informacji dodatkowej dobrana tak, żeby sprawozdania miały od 4 do 16 stron
	for i, notes := range []int{7, 24, 22, 38, 2} {
		write(fmt.Sprintf("esf-%d", i+1), statement(uint64(10+i), notes))
	}
}

// money formatuje grosze jak w polskich dokumentach: spacja tysięcy, przecinek dziesiętny.
func money(grosze int64) string {
	neg := grosze < 0
	if neg {
		grosze = -grosze
	}
	s := fmt.Sprintf("%d", grosze/100)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	r := fmt.Sprintf("%s,%02d", b.String(), grosze%100)
	if neg {
		r = "-" + r
	}
	return r
}

var (
	goods = []string{"Papier do drukarek A4, 80 g/m², ryza 500 arkuszy", "Toner do drukarki laserowej, czarny",
		"Segregator z mechanizmem dźwigniowym, 75 mm", "Usługa serwisowa urządzenia wielofunkcyjnego",
		"Kabel sieciowy kategorii 6, 5 m", "Licencja na oprogramowanie biurowe, 12 miesięcy",
		"Krzesło obrotowe z regulacją wysokości", "Monitor 27 cali, matryca IPS", "Klawiatura bezprzewodowa",
		"Szkolenie z obsługi systemu księgowego (1 dzień)", "Dysk SSD 1 TB", "Notes w kratkę A5, 96 kartek",
		"Długopis żelowy, niebieski, opakowanie 12 sztuk", "Transport i wniesienie towaru",
		"Konsultacja techniczna (godzina)", "Zszywacz biurowy do 40 kartek", "Etykiety samoprzylepne, 100 arkuszy",
		"Router dwupasmowy z obsługą sieci gościnnej", "Słuchawki z mikrofonem do wideokonferencji",
		"Przedłużacz z wyłącznikiem, 6 gniazd"}
	units = []string{"szt.", "szt.", "opak.", "usł.", "godz.", "kpl."}
)

type party struct{ name, nip, street, city, email string }

var (
	seller = party{"Przykładowa Spółka Handlowa sp. z o.o.", "1112223344", "ul. Przykładowa 15", "00-001 Warszawa", "biuro@example.com"}
	buyer  = party{"Odbiorca Testowy S.A.", "5556667788", "al. Zmyślona 120 lok. 4", "30-002 Kraków", "faktury@example.org"}
)

func partyHTML(title string, p party) string {
	return fmt.Sprintf(`<td><h2>%s</h2><div class="name">%s</div><div>NIP: %s</div><div>%s</div><div>%s</div><div class="muted">%s</div></td>`,
		title, html.EscapeString(p.name), p.nip, p.street, p.city, p.email)
}

// qr rysuje znak podobny do kodu QR (moduły losowe, trzy wzorce położenia); nie jest czytelnym kodem.
func qr(r *rand.Rand) string {
	const n = 29
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="40mm" height="40mm" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n+2, n+2, n+2, n+2)
	finder := func(x, y int) bool {
		in := func(cx, cy int) bool {
			dx, dy := x-cx, y-cy
			if dx < 0 || dy < 0 || dx > 6 || dy > 6 {
				return false
			}
			return dx == 0 || dy == 0 || dx == 6 || dy == 6 || (dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4)
		}
		return in(0, 0) || in(n-7, 0) || in(0, n-7)
	}
	reserved := func(x, y int) bool { return (x < 8 && y < 8) || (x >= n-8 && y < 8) || (x < 8 && y >= n-8) }
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if finder(x, y) || (!reserved(x, y) && r.IntN(2) == 0) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+1, y+1)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

const invoiceCSS = `@page { size: A4; margin: 12mm; }
html { font-family: "Noto Sans"; font-size: 9.5pt; color: #111; }
body { margin: 0; }
table { border-collapse: collapse; width: 100%; }
td, th { vertical-align: top; text-align: left; }
h1 { font-size: 16pt; margin: 0 0 1mm 0; }
h2 { font-size: 8pt; text-transform: uppercase; letter-spacing: 0.05em; color: #555; margin: 0 0 1.5mm 0; font-weight: bold; }
.number { font-size: 13pt; font-weight: bold; margin-bottom: 3mm; }
.muted { color: #555; }
.head td.qr { width: 52mm; text-align: center; }
.qrlabel { font-size: 7pt; white-space: nowrap; margin-top: 0.5mm; }
.parties { margin: 6mm 0 5mm 0; }
.parties td { width: 50%; padding-right: 6mm; }
.name { font-weight: bold; }
.lines th { font-size: 8pt; color: #555; font-weight: bold; border-bottom: 0.6pt solid #111; padding: 1.5mm; }
.lines td { border-bottom: 0.3pt solid #bbb; padding: 1.5mm; }
.lines .n, .rates .n { text-align: right; white-space: nowrap; }
.rates { width: 60%; margin: 4mm 0 0 40%; }
.rates th { font-size: 8pt; color: #555; font-weight: bold; padding: 1mm 1.5mm; border-bottom: 0.6pt solid #111; }
.rates td { padding: 1mm 1.5mm; }
.total { text-align: right; font-size: 12pt; font-weight: bold; margin-top: 4mm; }
.payment { margin-top: 4mm; }
.payment td { padding: 0.5mm 3mm 0.5mm 0; }
.footer { margin-top: 6mm; padding-top: 2mm; border-top: 0.3pt solid #bbb; font-size: 8pt; color: #333; }
.ksef { margin-top: 4mm; font-size: 8pt; color: #333; }
.link { word-break: break-all; }
`

func invoice(seed uint64, rows int, rates bool) string {
	r := rand.New(rand.NewPCG(seed, 2026))
	var b strings.Builder
	number := fmt.Sprintf("FV/2026/11/%04d", 100+seed)
	fmt.Fprintf(&b, "<!DOCTYPE html>\n<html lang=\"pl\"><head><meta charset=\"utf-8\"><title>Faktura %s</title><style>\n%s%s</style></head><body>\n", number, invoiceCSS, fontFaces)
	fmt.Fprintf(&b, `<table class="head"><tr><td><h1>Faktura</h1><div class="number">%s</div><div>Data wystawienia: 2026-11-%02d, Warszawa</div><div>Data dostawy lub wykonania usługi: 2026-11-%02d</div><div class="muted">Faktura wystawiona w Krajowym Systemie e-Faktur (dane przykładowe)</div></td><td class="qr">%s<div class="qrlabel">OFFLINE · kod weryfikacyjny</div></td></tr></table>`+"\n",
		number, 10+seed, 9+seed, qr(r))
	b.WriteString(`<table class="parties"><tr>` + partyHTML("Sprzedawca", seller) + partyHTML("Nabywca", buyer) + "</tr></table>\n")
	b.WriteString(`<h2>Pozycje faktury</h2><table class="lines"><thead><tr><th>Lp.</th><th>Nazwa towaru lub usługi</th><th class="n">Ilość</th><th>J.m.</th><th class="n">Cena netto</th><th class="n">Stawka</th><th class="n">Wartość netto</th></tr></thead><tbody>` + "\n")
	vat := []int{23, 23, 23, 8, 5, 0}
	net := map[int]int64{}
	order := []int{}
	for i := 0; i < rows; i++ {
		qty := int64(1 + r.IntN(12))
		price := int64(150 + r.IntN(250000))
		rate := 23
		if rates || r.IntN(8) == 0 {
			rate = vat[r.IntN(len(vat))]
		}
		value := qty * price
		if _, ok := net[rate]; !ok {
			order = append(order, rate)
		}
		net[rate] += value
		rateText := fmt.Sprintf("%d%%", rate)
		if rate == 0 {
			rateText = "0% WDT"
		}
		fmt.Fprintf(&b, `<tr><td>%d</td><td>%s</td><td class="n">%d</td><td>%s</td><td class="n">%s</td><td class="n">%s</td><td class="n">%s</td></tr>`+"\n",
			i+1, html.EscapeString(goods[r.IntN(len(goods))]), qty, units[r.IntN(len(units))], money(price), rateText, money(value))
	}
	b.WriteString("</tbody></table>\n")
	b.WriteString(`<table class="rates"><thead><tr><th>Stawka</th><th class="n">Netto</th><th class="n">VAT</th><th class="n">Brutto</th></tr></thead><tbody>`)
	var total int64
	for _, rate := range order {
		tax := net[rate] * int64(rate) / 100
		total += net[rate] + tax
		fmt.Fprintf(&b, `<tr><td>%d%%</td><td class="n">%s</td><td class="n">%s</td><td class="n">%s</td></tr>`, rate, money(net[rate]), money(tax), money(net[rate]+tax))
	}
	b.WriteString("</tbody></table>\n")
	fmt.Fprintf(&b, `<div class="total">Do zapłaty: %s PLN</div>`+"\n", money(total))
	if rates {
		fmt.Fprintf(&b, `<div class="payment"><h2>Płatność</h2><table><tr><td>Forma płatności</td><td>przelew</td></tr><tr><td>Termin płatności</td><td>2026-11-27</td></tr><tr><td>Rachunek bankowy</td><td>PL61 1090 1014 0000 0712 1981 2874</td></tr><tr><td>Bank</td><td>Bank Przykładowy S.A.</td></tr><tr><td>Tytuł przelewu</td><td>%s</td></tr></table></div>`+"\n", number)
	}
	fmt.Fprintf(&b, `<div class="ksef">Numer KSeF: %s-20261112-%010X-%02X<br>Weryfikacja: <span class="link">https://example.com/ksef/weryfikacja/%s/%016x%016x</span></div>`+"\n",
		seller.nip, r.Uint64()&0xffffffffff, r.IntN(256), seller.nip, r.Uint64(), r.Uint64())
	b.WriteString(`<div class="footer">Dokument przykładowy, dane zmyślone. Sprzedawca: ` + html.EscapeString(seller.name) + `, kapitał zakładowy 50 000,00 zł, sąd rejestrowy: Sąd Rejonowy (dane przykładowe).</div>` + "\n</body></html>\n")
	return b.String()
}

// Pozycje bilansu i rachunku zysków i strat według załącznika nr 1 do ustawy o rachunkowości
// (poziom zagnieżdżenia, oznaczenie, nazwa).
type item struct {
	level      int
	mark, name string
}

var assets = []item{
	{0, "A.", "Aktywa trwałe"}, {1, "I.", "Wartości niematerialne i prawne"}, {2, "1.", "Koszty zakończonych prac rozwojowych"},
	{2, "2.", "Wartość firmy"}, {2, "3.", "Inne wartości niematerialne i prawne"}, {2, "4.", "Zaliczki na wartości niematerialne i prawne"},
	{1, "II.", "Rzeczowe aktywa trwałe"}, {2, "1.", "Środki trwałe"}, {3, "a)", "grunty (w tym prawo użytkowania wieczystego gruntu)"},
	{3, "b)", "budynki, lokale, prawa do lokali i obiekty inżynierii lądowej i wodnej"}, {3, "c)", "urządzenia techniczne i maszyny"},
	{3, "d)", "środki transportu"}, {3, "e)", "inne środki trwałe"}, {2, "2.", "Środki trwałe w budowie"},
	{2, "3.", "Zaliczki na środki trwałe w budowie"}, {1, "III.", "Należności długoterminowe"}, {1, "IV.", "Inwestycje długoterminowe"},
	{2, "1.", "Nieruchomości"}, {2, "2.", "Wartości niematerialne i prawne"}, {2, "3.", "Długoterminowe aktywa finansowe"},
	{1, "V.", "Długoterminowe rozliczenia międzyokresowe"}, {2, "1.", "Aktywa z tytułu odroczonego podatku dochodowego"},
	{2, "2.", "Inne rozliczenia międzyokresowe"}, {0, "B.", "Aktywa obrotowe"}, {1, "I.", "Zapasy"}, {2, "1.", "Materiały"},
	{2, "2.", "Półprodukty i produkty w toku"}, {2, "3.", "Produkty gotowe"}, {2, "4.", "Towary"}, {2, "5.", "Zaliczki na dostawy i usługi"},
	{1, "II.", "Należności krótkoterminowe"}, {2, "1.", "Należności od jednostek powiązanych"},
	{2, "2.", "Należności od pozostałych jednostek, w których jednostka posiada zaangażowanie w kapitale"},
	{2, "3.", "Należności od pozostałych jednostek"}, {3, "a)", "z tytułu dostaw i usług, o okresie spłaty do 12 miesięcy"},
	{3, "b)", "z tytułu podatków, dotacji, ceł, ubezpieczeń społecznych i zdrowotnych oraz innych tytułów publicznoprawnych"},
	{1, "III.", "Inwestycje krótkoterminowe"}, {2, "1.", "Krótkoterminowe aktywa finansowe"}, {3, "a)", "środki pieniężne w kasie i na rachunkach"},
	{1, "IV.", "Krótkoterminowe rozliczenia międzyokresowe"}, {0, "C.", "Należne wpłaty na kapitał (fundusz) podstawowy"},
	{0, "D.", "Udziały (akcje) własne"},
}

var liabilities = []item{
	{0, "A.", "Kapitał (fundusz) własny"}, {1, "I.", "Kapitał (fundusz) podstawowy"}, {1, "II.", "Kapitał (fundusz) zapasowy, w tym:"},
	{2, "–", "nadwyżka wartości sprzedaży (wartości emisyjnej) nad wartością nominalną udziałów (akcji)"},
	{1, "III.", "Kapitał (fundusz) z aktualizacji wyceny"}, {1, "IV.", "Pozostałe kapitały (fundusze) rezerwowe"},
	{1, "V.", "Zysk (strata) z lat ubiegłych"}, {1, "VI.", "Zysk (strata) netto"}, {1, "VII.", "Odpisy z zysku netto w ciągu roku obrotowego (wielkość ujemna)"},
	{0, "B.", "Zobowiązania i rezerwy na zobowiązania"}, {1, "I.", "Rezerwy na zobowiązania"},
	{2, "1.", "Rezerwa z tytułu odroczonego podatku dochodowego"}, {2, "2.", "Rezerwa na świadczenia emerytalne i podobne"},
	{2, "3.", "Pozostałe rezerwy"}, {1, "II.", "Zobowiązania długoterminowe"}, {2, "1.", "Wobec jednostek powiązanych"},
	{2, "2.", "Wobec pozostałych jednostek"}, {3, "a)", "kredyty i pożyczki"}, {3, "b)", "z tytułu emisji dłużnych papierów wartościowych"},
	{1, "III.", "Zobowiązania krótkoterminowe"}, {2, "1.", "Zobowiązania wobec jednostek powiązanych"},
	{2, "2.", "Zobowiązania wobec pozostałych jednostek"}, {3, "a)", "kredyty i pożyczki"},
	{3, "b)", "z tytułu dostaw i usług, o okresie wymagalności do 12 miesięcy"}, {3, "c)", "zaliczki otrzymane na dostawy i usługi"},
	{3, "d)", "z tytułu podatków, ceł, ubezpieczeń społecznych i zdrowotnych oraz innych tytułów publicznoprawnych"},
	{3, "e)", "z tytułu wynagrodzeń"}, {2, "3.", "Fundusze specjalne"}, {1, "IV.", "Rozliczenia międzyokresowe"},
}

var income = []item{
	{0, "A.", "Przychody netto ze sprzedaży i zrównane z nimi, w tym:"}, {1, "–", "od jednostek powiązanych"},
	{1, "I.", "Przychody netto ze sprzedaży produktów"}, {1, "II.", "Zmiana stanu produktów (zwiększenie – wartość dodatnia, zmniejszenie – wartość ujemna)"},
	{1, "III.", "Koszt wytworzenia produktów na własne potrzeby jednostki"}, {1, "IV.", "Przychody netto ze sprzedaży towarów i materiałów"},
	{0, "B.", "Koszty działalności operacyjnej"}, {1, "I.", "Amortyzacja"}, {1, "II.", "Zużycie materiałów i energii"},
	{1, "III.", "Usługi obce"}, {1, "IV.", "Podatki i opłaty, w tym:"}, {2, "–", "podatek akcyzowy"}, {1, "V.", "Wynagrodzenia"},
	{1, "VI.", "Ubezpieczenia społeczne i inne świadczenia, w tym:"}, {2, "–", "emerytalne"}, {1, "VII.", "Pozostałe koszty rodzajowe"},
	{1, "VIII.", "Wartość sprzedanych towarów i materiałów"}, {0, "C.", "Zysk (strata) ze sprzedaży (A – B)"},
	{0, "D.", "Pozostałe przychody operacyjne"}, {0, "E.", "Pozostałe koszty operacyjne"}, {0, "F.", "Zysk (strata) z działalności operacyjnej (C + D – E)"},
	{0, "G.", "Przychody finansowe"}, {0, "H.", "Koszty finansowe"}, {0, "I.", "Zysk (strata) brutto (F + G – H)"},
	{0, "J.", "Podatek dochodowy"}, {0, "K.", "Pozostałe obowiązkowe zmniejszenia zysku (zwiększenia straty)"}, {0, "L.", "Zysk (strata) netto (I – J – K)"},
}

var cashflow = []item{
	{0, "A.", "Przepływy środków pieniężnych z działalności operacyjnej"}, {1, "I.", "Zysk (strata) netto"}, {1, "II.", "Korekty razem"},
	{2, "1.", "Amortyzacja"}, {2, "2.", "Zyski (straty) z tytułu różnic kursowych"}, {2, "3.", "Odsetki i udziały w zyskach (dywidendy)"},
	{2, "4.", "Zmiana stanu rezerw"}, {2, "5.", "Zmiana stanu zapasów"}, {2, "6.", "Zmiana stanu należności"},
	{2, "7.", "Zmiana stanu zobowiązań krótkoterminowych, z wyjątkiem pożyczek i kredytów"},
	{1, "III.", "Przepływy pieniężne netto z działalności operacyjnej (I ± II)"}, {0, "B.", "Przepływy środków pieniężnych z działalności inwestycyjnej"},
	{1, "I.", "Wpływy"}, {1, "II.", "Wydatki"}, {1, "III.", "Przepływy pieniężne netto z działalności inwestycyjnej (I – II)"},
	{0, "C.", "Przepływy środków pieniężnych z działalności finansowej"}, {1, "I.", "Wpływy"}, {1, "II.", "Wydatki"},
	{1, "III.", "Przepływy pieniężne netto z działalności finansowej (I – II)"}, {0, "D.", "Przepływy pieniężne netto, razem (A.III ± B.III ± C.III)"},
	{0, "E.", "Bilansowa zmiana stanu środków pieniężnych, w tym:"}, {0, "F.", "Środki pieniężne na początek okresu"},
	{0, "G.", "Środki pieniężne na koniec okresu (F ± D)"},
}

var noteTitles = []string{"Zasady (polityka) rachunkowości", "Zmiany wartości niematerialnych i prawnych", "Zmiany środków trwałych",
	"Struktura należności według okresów spłaty", "Odpisy aktualizujące wartość należności", "Struktura kapitału podstawowego",
	"Propozycja podziału zysku za rok obrotowy", "Rezerwy według celów utworzenia", "Zobowiązania długoterminowe według okresu spłaty",
	"Rozliczenia międzyokresowe", "Struktura przychodów według rodzajów działalności", "Struktura przychodów według rynków geograficznych",
	"Koszty według rodzajów", "Podatek dochodowy: różnice między podstawą opodatkowania a wynikiem brutto", "Przeciętne zatrudnienie",
	"Wynagrodzenie biegłego rewidenta", "Zdarzenia po dniu bilansowym", "Transakcje z jednostkami powiązanymi"}

var sentences = []string{"Spółka sporządza sprawozdanie finansowe przy założeniu kontynuowania działalności w dającej się przewidzieć przyszłości.",
	"Środki trwałe wycenia się według cen nabycia lub kosztów wytworzenia, pomniejszonych o odpisy amortyzacyjne oraz odpisy z tytułu trwałej utraty wartości.",
	"Należności wycenia się w kwocie wymaganej zapłaty, z zachowaniem zasady ostrożności, z uwzględnieniem odpisów aktualizujących.",
	"Zapasy wycenia się według cen nabycia nie wyższych od cen sprzedaży netto; rozchód wycenia się metodą „pierwsze weszło, pierwsze wyszło”.",
	"Rezerwy tworzy się na pewne lub o dużym stopniu prawdopodobieństwa przyszłe zobowiązania, których kwotę można w sposób wiarygodny oszacować.",
	"W roku obrotowym nie wystąpiły zdarzenia, które wymagałyby ujęcia w sprawozdaniu jako zdarzenia nadzwyczajne.",
	"Przychody ze sprzedaży ujmuje się w momencie przeniesienia na nabywcę znaczących ryzyk i korzyści wynikających z prawa własności.",
	"Kursy walut obcych przyjęte do wyceny pozycji bilansu odpowiadają średnim kursom ogłoszonym przez bank centralny na dzień bilansowy.",
	"Dane porównawcze za poprzedni rok obrotowy zostały przekształcone tak, aby zapewnić ich porównywalność z danymi roku bieżącego.",
	"Zarząd nie stwierdził okoliczności wskazujących na zagrożenie kontynuowania działalności przez jednostkę."}

const statementCSS = `@page { size: A4; margin: 18mm 12mm 16mm 12mm;
  @top-left { content: string(company); font-size: 7.5pt; color: #555; }
  @top-right { content: "Sprawozdanie finansowe za rok 2025"; font-size: 7.5pt; color: #555; }
  @bottom-right { content: "Strona " counter(page) " z " counter(pages); font-size: 7.5pt; color: #555; } }
html { font-family: "Noto Sans"; font-size: 9pt; line-height: 1.35; color: #1f2937; }
body { margin: 0; }
h1 { font-size: 15pt; margin: 0 0 2mm; string-set: company content(); }
h2 { font-size: 11.5pt; margin: 6mm 0 2mm; padding-bottom: 1mm; border-bottom: 0.6pt solid #9ca3af; break-after: avoid; }
h3 { font-size: 9.5pt; margin: 4mm 0 1.5mm; break-after: avoid; }
.meta { display: grid; grid-template-columns: max-content 1fr; gap: 1mm 5mm; margin: 3mm 0; }
.meta dt { color: #6b7280; }
.meta dd { margin: 0; }
.status { display: flex; flex-wrap: wrap; gap: 2mm; margin: 3mm 0; }
.badge { display: inline-flex; align-items: center; gap: 1mm; border: 0.5pt solid #d1d5db; border-radius: 3mm; padding: 0.6mm 2.5mm; font-size: 8pt; }
.badge svg { width: 3.2mm; height: 3.2mm; flex-shrink: 0; }
table.fs { width: 100%; border-collapse: collapse; margin: 1mm 0 3mm; }
table.fs th { font-size: 7.5pt; text-align: right; color: #4b5563; border-bottom: 0.6pt solid #374151; padding: 1mm 1.5mm; vertical-align: bottom; }
table.fs th:first-child, table.fs th:nth-child(2) { text-align: left; }
table.fs td { padding: 0.8mm 1.5mm; border-bottom: 0.3pt solid #e5e7eb; vertical-align: top; }
table.fs td.n { text-align: right; white-space: nowrap; font-family: "Noto Sans Mono"; font-size: 8pt; }
table.fs td.m { white-space: nowrap; color: #4b5563; }
table.fs tr.l0 td { font-weight: bold; background: #f3f4f6; }
table.fs tr.l1 td.t { padding-left: 4mm; }
table.fs tr.l2 td.t { padding-left: 8mm; }
table.fs tr.l3 td.t { padding-left: 12mm; font-style: italic; }
table.fs tr { break-inside: avoid; }
table.fs thead { display: table-header-group; }
details { margin: 2mm 0; border: 0.4pt solid #e5e7eb; border-radius: 1.5mm; padding: 1mm 3mm; }
summary { font-weight: bold; }
.note p { margin: 1mm 0; text-align: justify; hyphens: auto; }
.sign { display: flex; justify-content: space-between; margin-top: 12mm; break-inside: avoid; }
.sign div { width: 45%; border-top: 0.4pt solid #6b7280; padding-top: 1mm; font-size: 8pt; text-align: center; }
`

const iconOK = `<svg viewBox="0 0 16 16"><path d="M8 1a7 7 0 1 0 0 14A7 7 0 0 0 8 1zm-1 10.4L3.6 8l1.1-1.1L7 9.2l4.3-4.3 1.1 1.1z" fill="#16a34a"/></svg>`
const iconInfo = `<svg viewBox="0 0 16 16"><path d="M8 1a7 7 0 1 0 0 14A7 7 0 0 0 8 1zm.8 11H7.2V7h1.6zm0-6.4H7.2V4h1.6z" fill="#0284c7"/></svg>`

func statement(seed uint64, notes int) string {
	r := rand.New(rand.NewPCG(seed, 2025))
	companies := []string{"Zakłady Przykładowe", "Testowa Produkcja", "Wzorcowe Usługi", "Fikcyjny Handel", "Modelowa Logistyka"}
	company := companies[int(seed)%len(companies)] + " sp. z o.o."
	var b strings.Builder
	fmt.Fprintf(&b, "<!DOCTYPE html>\n<html lang=\"pl\"><head><meta charset=\"utf-8\"><title>Sprawozdanie finansowe %s</title><style>\n%s%s</style></head><body>\n", html.EscapeString(company), statementCSS, fontFaces)
	fmt.Fprintf(&b, "<h1>%s</h1>\n<div>Sprawozdanie finansowe za okres od 2025-01-01 do 2025-12-31 (dane zmyślone)</div>\n", html.EscapeString(company))
	b.WriteString(`<div class="status"><span class="badge">` + iconOK + `Zgodne ze schematem</span><span class="badge">` + iconOK + `Sumy kontrolne poprawne</span><span class="badge">` + iconInfo + `Wariant porównawczy rachunku zysków i strat</span></div>` + "\n")
	b.WriteString(`<h2>Wprowadzenie do sprawozdania finansowego</h2><dl class="meta">`)
	for _, kv := range [][2]string{{"Nazwa", company}, {"Siedziba", "Warszawa, mazowieckie"}, {"Adres", "ul. Przykładowa 15, 00-001 Warszawa"},
		{"KRS", fmt.Sprintf("00009%05d", r.IntN(100000))}, {"NIP", fmt.Sprintf("%010d", r.Int64N(1e10))}, {"Rodzaj działalności (PKD)", "46.66.Z Sprzedaż hurtowa pozostałych maszyn i urządzeń biurowych"},
		{"Okres", "2025-01-01 – 2025-12-31"}, {"Dzień bilansowy", "2025-12-31"}, {"Kontynuacja działalności", "tak"}, {"Jednostka", "złote polskie"}} {
		fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>", kv[0], html.EscapeString(kv[1]))
	}
	b.WriteString("</dl>\n<p class=\"note\">" + sentences[0] + " " + sentences[1] + " " + sentences[2] + "</p>\n")
	table := func(title string, items []item, cols [2]string) {
		fmt.Fprintf(&b, "<h2>%s</h2>\n<table class=\"fs\"><colgroup><col style=\"width:9mm\"><col><col style=\"width:30mm\"><col style=\"width:30mm\"></colgroup><thead><tr><th>Poz.</th><th>Wyszczególnienie</th><th>%s</th><th>%s</th></tr></thead><tbody>\n", title, cols[0], cols[1])
		for _, it := range items {
			scale := []int64{9e9, 2e9, 6e8, 1e8}[it.level]
			a, p := r.Int64N(scale), r.Int64N(scale)
			if r.IntN(9) == 0 {
				a = 0
			}
			fmt.Fprintf(&b, "<tr class=\"l%d\"><td class=\"m\">%s</td><td class=\"t\">%s</td><td class=\"n\">%s</td><td class=\"n\">%s</td></tr>\n",
				it.level, it.mark, html.EscapeString(it.name), money(a), money(p))
		}
		b.WriteString("</tbody></table>\n")
	}
	years := [2]string{"Stan na 2025-12-31", "Stan na 2024-12-31"}
	table("Bilans – aktywa", assets, years)
	table("Bilans – pasywa", liabilities, years)
	table("Rachunek zysków i strat (wariant porównawczy)", income, [2]string{"2025", "2024"})
	table("Rachunek przepływów pieniężnych (metoda pośrednia)", cashflow, [2]string{"2025", "2024"})
	b.WriteString("<h2>Informacja dodatkowa</h2>\n")
	for i := 0; i < notes; i++ {
		fmt.Fprintf(&b, "<details open class=\"note\"><summary>Nota %d. %s</summary>\n", i+1, noteTitles[i%len(noteTitles)])
		for p := 0; p < 1+r.IntN(3); p++ {
			b.WriteString("<p>")
			for s := 0; s < 2+r.IntN(4); s++ {
				b.WriteString(sentences[r.IntN(len(sentences))] + " ")
			}
			b.WriteString("</p>\n")
		}
		if r.IntN(3) > 0 {
			sub := liabilities[1+r.IntN(len(liabilities)-8):]
			sub = sub[:4+r.IntN(5)]
			b.WriteString("<table class=\"fs\"><thead><tr><th>Poz.</th><th>Wyszczególnienie</th><th>Bilans otwarcia</th><th>Bilans zamknięcia</th></tr></thead><tbody>\n")
			for _, it := range sub {
				fmt.Fprintf(&b, "<tr class=\"l%d\"><td class=\"m\">%s</td><td class=\"t\">%s</td><td class=\"n\">%s</td><td class=\"n\">%s</td></tr>\n",
					min(it.level, 3), it.mark, html.EscapeString(it.name), money(r.Int64N(5e8)), money(r.Int64N(5e8)))
			}
			b.WriteString("</tbody></table>\n")
		}
		b.WriteString("</details>\n")
	}
	b.WriteString(`<div class="sign"><div>Osoba, której powierzono prowadzenie ksiąg rachunkowych<br>Anna Przykładowa</div><div>Zarząd<br>Jan Testowy, Prezes Zarządu</div></div>` + "\n</body></html>\n")
	return b.String()
}
