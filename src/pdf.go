package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

// ReportTemplate controls the visual style of generated PDF reports.
type ReportTemplate struct {
	Cover  CoverConfig  `yaml:"cover"`
	Body   BodyConfig   `yaml:"body"`
	Header HeaderConfig `yaml:"header"`
	Footer FooterConfig `yaml:"footer"`
	Brand  BrandConfig  `yaml:"brand"`
}

type CoverConfig struct {
	BackgroundColor string `yaml:"background_color"`
	TitleColor      string `yaml:"title_color"`
	SubtitleColor   string `yaml:"subtitle_color"`
	AccentColor     string `yaml:"accent_color"`
	Logo            string `yaml:"logo"`
}

type BodyConfig struct {
	FontSize    float64 `yaml:"font_size"`
	H2Color     string  `yaml:"h2_color"`
	H3Color     string  `yaml:"h3_color"`
	TextColor   string  `yaml:"text_color"`
	BulletColor string  `yaml:"bullet_color"`
}

type HeaderConfig struct {
	Text  string `yaml:"text"`
	Color string `yaml:"color"`
}

type FooterConfig struct {
	Left            string `yaml:"left"`
	ShowPageNumbers bool   `yaml:"show_page_numbers"`
	Color           string `yaml:"color"`
}

type BrandConfig struct {
	Name string `yaml:"name"`
}

// loadReportTemplate resolves the template in priority order:
//  1. projectDir/template.yaml
//  2. userBaseDir/template.yaml
//  3. ~/.config/bot/report-template/template.yaml
//
// Falls back to a hardcoded default if none found.
func loadReportTemplate(projectDir, userBaseDir string) (*ReportTemplate, error) {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(projectDir, "template.yaml"),
		filepath.Join(userBaseDir, "template.yaml"),
		filepath.Join(home, ".config", "bot", "report-template", "template.yaml"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var tmpl ReportTemplate
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			continue
		}
		return &tmpl, nil
	}
	return defaultReportTemplate(), nil
}

// copyReportTemplate copies the global default template to a new user's base dir.
// Called when a user is approved so they start with the Artoo style.
func copyReportTemplate(baseDir string, userID int64) {
	home, _ := os.UserHomeDir()
	globalTmpl := filepath.Join(home, ".config", "bot", "report-template", "template.yaml")
	userTmpl := filepath.Join(userWorkingDir(baseDir, userID), "template.yaml")
	if _, err := os.Stat(userTmpl); os.IsNotExist(err) {
		if data, err := os.ReadFile(globalTmpl); err == nil {
			os.WriteFile(userTmpl, data, 0644)
		}
	}
}

func defaultReportTemplate() *ReportTemplate {
	return &ReportTemplate{
		Cover: CoverConfig{
			BackgroundColor: "#0f0f23",
			TitleColor:      "#ffffff",
			SubtitleColor:   "#aaaaaa",
			AccentColor:     "#4a9eff",
		},
		Body: BodyConfig{
			FontSize:    11,
			H2Color:     "#2d3561",
			H3Color:     "#555588",
			TextColor:   "#333333",
			BulletColor: "#4a9eff",
		},
		Header: HeaderConfig{
			Text:  "Artoo Reports",
			Color: "#aaaaaa",
		},
		Footer: FooterConfig{
			Left:            "Artoo Reports",
			ShowPageNumbers: true,
			Color:           "#aaaaaa",
		},
		Brand: BrandConfig{
			Name: "Artoo Reports",
		},
	}
}

// hexToRGB converts a "#rrggbb" hex string to r, g, b int components (0-255).
func hexToRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

// pdfText converts a UTF-8 string to Windows-1252 so fpdf's built-in fonts
// render characters correctly. fpdf built-in fonts (Helvetica, Courier) use
// single-byte Latin-1/cp1252 encoding — passing raw UTF-8 multi-byte sequences
// produces garbled output (e.g. ö → Ã¶).
//
// Conversion rules:
//   - U+0000–U+007F: pass through as-is (ASCII)
//   - U+0080–U+00FF: emit as single byte (Latin-1 range; covers ö ä å Ö Ä Å etc.)
//   - cp1252-specific typographic chars (em dash, curly quotes, ellipsis, bullet):
//     map to their Windows-1252 byte values (0x80–0x9F range)
//   - Everything else (U+0100+): drop (not representable in cp1252)
func pdfText(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\u2014':
			buf.WriteByte('\x97') // em dash —
		case '\u2013':
			buf.WriteByte('\x96') // en dash –
		case '\u2019':
			buf.WriteByte('\x92') // right single quote '
		case '\u2018':
			buf.WriteByte('\x91') // left single quote '
		case '\u201C':
			buf.WriteByte('\x93') // left double quote "
		case '\u201D':
			buf.WriteByte('\x94') // right double quote "
		case '\u2026':
			buf.WriteByte('\x85') // ellipsis …
		case '\u00A0':
			buf.WriteByte(' ') // non-breaking space
		case '\u2022':
			buf.WriteByte('\x95') // bullet •
		default:
			if r < 0x80 {
				buf.WriteByte(byte(r))
			} else if r <= 0xFF {
				// Latin-1 range: Unicode code point == cp1252 byte value.
				// Covers Swedish/Nordic: ö(F6) ä(E4) å(E5) Ö(D6) Ä(C4) Å(C5)
				// and all other accented Latin characters in this range.
				buf.WriteByte(byte(r))
			}
			// U+0100 and above: not representable in cp1252 — silently drop.
		}
	}
	return buf.String()
}

// styledSpan is a run of text with optional inline formatting.
type styledSpan struct {
	text   string
	bold   bool
	italic bool
	code   bool
}

// extractStyledSpans walks a goldmark AST node and returns inline-styled text spans.
func extractStyledSpans(n ast.Node, source []byte) []styledSpan {
	var spans []styledSpan
	bold := false
	italic := false
	inCode := false
	ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		switch v := node.(type) {
		case *ast.Emphasis:
			if v.Level == 2 {
				bold = entering
			} else {
				italic = entering
			}
		case *ast.CodeSpan:
			inCode = entering
		case *ast.Text:
			if entering {
				t := string(v.Segment.Value(source))
				if v.SoftLineBreak() {
					t += " "
				}
				if t != "" {
					spans = append(spans, styledSpan{text: t, bold: bold, italic: italic, code: inCode})
				}
			}
		case *ast.String:
			if entering && len(v.Value) > 0 {
				spans = append(spans, styledSpan{text: string(v.Value), bold: bold, italic: italic, code: inCode})
			}
		}
		return ast.WalkContinue, nil
	})
	return spans
}

// extractPlainText walks a goldmark AST node and returns all text as a plain string.
func extractPlainText(n ast.Node, source []byte) string {
	var parts []string
	ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := node.(type) {
		case *ast.Text:
			t := string(v.Segment.Value(source))
			if v.SoftLineBreak() {
				t += " "
			}
			parts = append(parts, t)
		case *ast.String:
			if len(v.Value) > 0 {
				parts = append(parts, string(v.Value))
			}
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(strings.Join(parts, ""))
}

// extractCodeLines returns the raw source lines from a fenced/indented code block.
func extractCodeLines(n ast.Node, source []byte) string {
	var buf strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		buf.Write(seg.Value(source))
	}
	return strings.TrimRight(buf.String(), "\n")
}

// RenderMarkdownReport reads mdPath, renders it as a styled PDF and writes to outPath.
// The first H1 becomes the cover page title; everything after is the body.
func RenderMarkdownReport(mdPath, outPath string, tmpl *ReportTemplate) error {
	source, err := os.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", mdPath, err)
	}

	md := goldmark.New()
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	// Separate H1 (cover title) from body nodes.
	title := ""
	var bodyNodes []ast.Node
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok && h.Level == 1 && title == "" {
			title = extractPlainText(n, source)
			continue
		}
		bodyNodes = append(bodyNodes, n)
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(mdPath), ".md")
	}

	fontSize := tmpl.Body.FontSize
	if fontSize <= 0 {
		fontSize = 11
	}
	lineH := fontSize * 0.45 // approximate line height in mm

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 25, 20)
	pdf.SetAutoPageBreak(true, 20)

	// Capture config values for use in closures (avoid holding tmpl reference issues).
	headerText := tmpl.Header.Text
	headerColor := tmpl.Header.Color
	footerLeft := tmpl.Footer.Left
	footerColor := tmpl.Footer.Color
	footerPages := tmpl.Footer.ShowPageNumbers

	pdf.SetHeaderFunc(func() {
		if pdf.PageNo() == 1 {
			return // no header on cover page
		}
		r, g, b := hexToRGB(headerColor)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(r, g, b)
		pdf.SetXY(0, 8)
		pdf.CellFormat(210, 5, pdfText(headerText), "", 1, "C", false, 0, "")
		pdf.SetDrawColor(r, g, b)
		pdf.Line(20, 15, 190, 15)
	})

	pdf.SetFooterFunc(func() {
		if pdf.PageNo() == 1 {
			return // no footer on cover page
		}
		r, g, b := hexToRGB(footerColor)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(r, g, b)
		pdf.SetY(-15)
		pdf.SetDrawColor(r, g, b)
		pdf.Line(20, pdf.GetY()-2, 190, pdf.GetY()-2)
		pdf.SetX(20)
		pdf.Cell(90, 5, pdfText(footerLeft))
		if footerPages {
			// Body starts at page 2, so display as page N-1.
			pdf.CellFormat(80, 5, fmt.Sprintf("Page %d", pdf.PageNo()-1), "", 0, "R", false, 0, "")
		}
	})

	renderCoverPage(pdf, title, tmpl)

	if len(bodyNodes) > 0 {
		pdf.AddPage()
		pdf.SetY(25) // ensure body starts at content top after cover page state
		renderBodyNodes(pdf, bodyNodes, source, tmpl, fontSize, lineH)
	}

	return pdf.OutputFileAndClose(outPath)
}

func renderCoverPage(pdf *fpdf.Fpdf, title string, tmpl *ReportTemplate) {
	pdf.AddPage()

	// Full-page background fill.
	r, g, b := hexToRGB(tmpl.Cover.BackgroundColor)
	pdf.SetFillColor(r, g, b)
	pdf.Rect(0, 0, 210, 297, "F")

	y := 80.0

	// Optional logo, centered.
	if tmpl.Cover.Logo != "" {
		if _, err := os.Stat(tmpl.Cover.Logo); err == nil {
			imgW := 40.0
			imgX := (210 - imgW) / 2
			pdf.Image(tmpl.Cover.Logo, imgX, y, imgW, 0, false, "", 0, "")
			y += 50
		}
	}

	// Accent bar.
	ar, ag, ab := hexToRGB(tmpl.Cover.AccentColor)
	pdf.SetFillColor(ar, ag, ab)
	pdf.Rect(20, y, 170, 3, "F")
	y += 12

	// Title.
	tr, tg, tb := hexToRGB(tmpl.Cover.TitleColor)
	pdf.SetTextColor(tr, tg, tb)
	pdf.SetFont("Helvetica", "B", 26)
	pdf.SetXY(20, y)
	pdf.MultiCell(170, 11, pdfText(title), "", "L", false)
	y = pdf.GetY() + 6

	// Date subtitle.
	sr, sg, sb := hexToRGB(tmpl.Cover.SubtitleColor)
	pdf.SetTextColor(sr, sg, sb)
	pdf.SetFont("Helvetica", "", 12)
	pdf.SetXY(20, y)
	pdf.Cell(170, 8, time.Now().Format("January 2, 2006"))

	// Brand name at bottom right.
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(20, 278)
	pdf.CellFormat(170, 8, pdfText(tmpl.Brand.Name), "", 0, "R", false, 0, "")
}

func renderBodyNodes(pdf *fpdf.Fpdf, nodes []ast.Node, source []byte, tmpl *ReportTemplate, fontSize, lineH float64) {
	for _, n := range nodes {
		switch v := n.(type) {
		case *ast.Heading:
			renderHeading(pdf, v, source, tmpl, fontSize)

		case *ast.Paragraph:
			renderParagraph(pdf, n, source, tmpl, fontSize, lineH)

		case *ast.List:
			renderList(pdf, v, source, tmpl, fontSize, lineH)

		case *ast.ThematicBreak:
			cr, cg, cb := hexToRGB("#cccccc")
			pdf.SetDrawColor(cr, cg, cb)
			pdf.Ln(3)
			pdf.Line(20, pdf.GetY(), 190, pdf.GetY())
			pdf.Ln(5)

		case *ast.FencedCodeBlock, *ast.CodeBlock:
			code := extractCodeLines(n, source)
			pdf.SetFont("Courier", "", fontSize-1)
			cr, cg, cb := hexToRGB("#555555")
			pdf.SetTextColor(cr, cg, cb)
			pdf.SetFillColor(245, 245, 245)
			pdf.MultiCell(170, lineH, code, "1", "L", true)
			pdf.Ln(3)

		case *ast.Blockquote:
			qt := pdfText(extractPlainText(n, source))
			cr, cg, cb := hexToRGB("#666666")
			pdf.SetTextColor(cr, cg, cb)
			pdf.SetFont("Helvetica", "I", fontSize)
			pdf.SetX(26)
			pdf.MultiCell(162, lineH, qt, "L", "L", false)
			pdf.Ln(2)
		}
	}
}

func renderHeading(pdf *fpdf.Fpdf, h *ast.Heading, source []byte, tmpl *ReportTemplate, baseFontSize float64) {
	headingText := pdfText(extractPlainText(h, source))
	switch h.Level {
	case 2:
		pdf.Ln(6)
		r, g, b := hexToRGB(tmpl.Body.H2Color)
		pdf.SetTextColor(r, g, b)
		pdf.SetFont("Helvetica", "B", baseFontSize+5)
		pdf.SetX(20)
		pdf.MultiCell(170, 9, headingText, "", "L", false)
		pdf.SetDrawColor(r, g, b)
		pdf.Line(20, pdf.GetY(), 190, pdf.GetY())
		pdf.Ln(3)
	case 3:
		pdf.Ln(4)
		r, g, b := hexToRGB(tmpl.Body.H3Color)
		pdf.SetTextColor(r, g, b)
		pdf.SetFont("Helvetica", "B", baseFontSize+2)
		pdf.SetX(20)
		pdf.MultiCell(170, 7, headingText, "", "L", false)
		pdf.Ln(1)
	default: // h4+
		pdf.Ln(3)
		r, g, b := hexToRGB(tmpl.Body.H3Color)
		pdf.SetTextColor(r, g, b)
		pdf.SetFont("Helvetica", "B", baseFontSize)
		pdf.SetX(20)
		pdf.MultiCell(170, 6, headingText, "", "L", false)
		pdf.Ln(1)
	}
}

func renderParagraph(pdf *fpdf.Fpdf, n ast.Node, source []byte, tmpl *ReportTemplate, fontSize, lineH float64) {
	spans := extractStyledSpans(n, source)
	if len(spans) == 0 {
		return
	}
	tr, tg, tb := hexToRGB(tmpl.Body.TextColor)
	pdf.SetX(20)
	for _, span := range spans {
		if span.text == "" {
			continue
		}
		pdf.SetTextColor(tr, tg, tb)
		switch {
		case span.code:
			pdf.SetFont("Courier", "", fontSize-1)
		case span.bold && span.italic:
			pdf.SetFont("Helvetica", "BI", fontSize)
		case span.bold:
			pdf.SetFont("Helvetica", "B", fontSize)
		case span.italic:
			pdf.SetFont("Helvetica", "I", fontSize)
		default:
			pdf.SetFont("Helvetica", "", fontSize)
		}
		pdf.Write(lineH, pdfText(span.text))
	}
	pdf.Ln(lineH + 2)
}

func renderList(pdf *fpdf.Fpdf, list *ast.List, source []byte, tmpl *ReportTemplate, fontSize, lineH float64) {
	ordered := list.IsOrdered()
	itemNum := 1
	if ordered && list.Start > 0 {
		itemNum = list.Start
	}
	tr, tg, tb := hexToRGB(tmpl.Body.TextColor)
	br, bg, bb := hexToRGB(tmpl.Body.BulletColor)

	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		itemText := pdfText(extractPlainText(item, source))
		if itemText == "" {
			continue
		}
		pdf.SetFont("Helvetica", "B", fontSize)
		pdf.SetTextColor(br, bg, bb)
		pdf.SetX(20)
		if ordered {
			pdf.Cell(8, lineH, fmt.Sprintf("%d.", itemNum))
			itemNum++
		} else {
			pdf.Cell(6, lineH, "-")
		}
		pdf.SetTextColor(tr, tg, tb)
		pdf.SetFont("Helvetica", "", fontSize)
		pdf.SetX(28)
		pdf.MultiCell(162, lineH, itemText, "", "L", false)
	}
	pdf.Ln(3)
}
