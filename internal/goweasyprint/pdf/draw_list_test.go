package pdf

import (
	"fmt"
	"testing"

	"github.com/polestorm/gopdfgo/internal/webrender/utils/testutils"
)

// Test how lists are drawn.

func TestListStyleImage(t *testing.T) {
	defer testutils.CaptureLogs().AssertNoLogs(t)

	doTest := func(position, pixels string) {
		assertPixelsEqual(t, pixels, fmt.Sprintf(`
		<style>
		  @page { size: 16px 10px }
		  body { margin: 0; background: white; font-family: %s }
		  ul { margin: 2px 2px 0 11px; list-style: url(../resources_test/pattern.png) %s;
			   font-size: 2px }
		</style>
		<ul><li></li></ul>`, sansFonts, position))
	}

	// gopdfgo: obrazek znacznika zewnętrznego stoi 7 px przed treścią, jak w Chromium
	// (kCMarkerPaddingPx); źródło brało 0.5em. Strona i lewy margines poszerzone o 4 px, żeby
	// obrazek mieścił się na stronie.
	doTest("outside",
		// <li> horizontal margins: 11px 2px, width: 16 - 11 - 2 = 3px
		// list marker padding: 7px, image is 4px wide: 11 - 7 - 4 = 0
		`
        ________________
        ________________
        rBBB____________
        BBBB____________
        BBBB____________
        BBBB____________
        ________________
        ________________
        ________________
        ________________
     `)
	doTest("inside",
		// <li> horizontal margins: 11px 2px, width: 3px; marker image 4px wide: overflow
		`
        ________________
        ________________
        ___________rBBB_
        ___________BBBB_
        ___________BBBB_
        ___________BBBB_
        ________________
        ________________
        ________________
        ________________
     `)
}

func TestListStyleImageNone(t *testing.T) {
	defer testutils.CaptureLogs().AssertNoLogs(t)

	assertPixelsEqual(t, `
        __________
        __________
        __________
        __________
        __________
        __________
        __________
        __________
        __________
        __________
    `, fmt.Sprintf(`
      <style>
        @page { size: 10px }
        body { margin: 0; background: white; font-family: %s }
        ul { margin: 0 0 0 5px; list-style: none; font-size: 2px; }
      </style>
      <ul><li>`, sansFonts))
}
