package fonts

import (
	_ "embed"
	"fmt"
	"log"

	"github.com/go-text/typesetting/fontscan"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	resourcestest "github.com/polestorm/gopdfgo/internal/webrender/resources_test"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

const (
	fontmapCacheGotext = "../../text/testdata"

	useGoText = true
)

// FontConfig is loaded with [UAStylesheet]
var FontConfig text.FontConfiguration

func init() {
	if useGoText {
		fontmapGotext := fontscan.NewFontMap(log.Default())
		err := fontmapGotext.UseSystemFonts(fontmapCacheGotext)
		if err != nil {
			panic(err)
		}
		FontConfig = text.NewFontConfigurationGotext(fontmapGotext)
	}

	// baseUrl, err := utils.PathToURL("../../resources_test/")
	// if err != nil {
	// 	panic(err)
	// }

	// UAStylesheet, err = tree.NewCSSExt(utils.InputString(resourcestest.TestUACSS), baseUrl, FontConfig)
	// if err != nil {
	// 	panic(fmt.Sprintf("invalid embedded stylesheet: %s", err))
	// }
}

func UAStylesheet(baseURL string) tree.CSS {
	out, err := tree.NewCSSExt(utils.InputString(resourcestest.TestUACSS), baseURL, FontConfig)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded stylesheet: %s", err))
	}
	return out
}
