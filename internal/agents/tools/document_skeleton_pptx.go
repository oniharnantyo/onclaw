package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The embedded pptx skeleton (design.md D2): one master, one layout, one
// theme, compiled into the binary as XML constants and cloned per call —
// invisible infrastructure, never a user-managed template. House style uses
// the design contract's tokens: accent #2f6feb, fg #111111, bg #fafafa.
// v1 decks are title + content slides only.

const pptxContentTypesHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>
<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>
<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>`

const pptxContentTypesFooter = `</Types>`

const pptxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
</Relationships>`

// pptxPresentationXML builds presentation.xml with one sldId per slide
// (ids start at 256; rId1 is the slide master, slides take rId2..).
func pptxPresentationXML(slideCount int) string {
	var sldIds strings.Builder
	for i := 0; i < slideCount; i++ {
		sldIds.WriteString(`<p:sldId id="` + strconv.Itoa(256+i) + `" r:id="rId` + strconv.Itoa(i+2) + `"/>`)
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" saveSubsetFonts="1">
<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>
<p:sldIdLst>` + sldIds.String() + `</p:sldIdLst>
<p:sldSz cx="12192000" cy="6858000"/>
<p:notesSz cx="6858000" cy="9144000"/>
</p:presentation>`
}

// pptxPresentationRels builds ppt/_rels/presentation.xml.rels: the master
// plus one relationship per slide.
func pptxPresentationRels(slideCount int) string {
	var rels strings.Builder
	rels.WriteString(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>`)
	for i := 0; i < slideCount; i++ {
		rels.WriteString(`<Relationship Id="rId` + strconv.Itoa(i+2) + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide` + strconv.Itoa(i+1) + `.xml"/>`)
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`
}

const pptxSlideMasterXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
<p:cSld>
<p:spTree>
<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>
<p:sp>
<p:nvSpPr><p:cNvPr id="2" name="Title Placeholder"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="838200" y="365125"/><a:ext cx="10515600" cy="1325563"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody>
</p:sp>
<p:sp>
<p:nvSpPr><p:cNvPr id="3" name="Body Placeholder"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="838200" y="1825625"/><a:ext cx="10515600" cy="4351338"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody>
</p:sp>
</p:spTree>
</p:cSld>
<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>
<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>
</p:sldMaster>`

const pptxSlideMasterRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>
</Relationships>`

const pptxSlideLayoutXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="obj" preserve="1">
<p:cSld name="Title and Content">
<p:spTree>
<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>
</p:spTree>
</p:cSld>
<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>
</p:sldLayout>`

const pptxSlideLayoutRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>
</Relationships>`

// pptxThemeXML is the house theme; fmtScheme carries the three-entry style
// lists the OOXML schema requires.
const pptxThemeXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="OnClaw">
<a:themeElements>
<a:clrScheme name="OnClaw">
<a:dk1><a:srgbClr val="111111"/></a:dk1>
<a:lt1><a:srgbClr val="FFFFFF"/></a:lt1>
<a:dk2><a:srgbClr val="1F2937"/></a:dk2>
<a:lt2><a:srgbClr val="FAFAFA"/></a:lt2>
<a:accent1><a:srgbClr val="2F6FEB"/></a:accent1>
<a:accent2><a:srgbClr val="6B7280"/></a:accent2>
<a:accent3><a:srgbClr val="10B981"/></a:accent3>
<a:accent4><a:srgbClr val="F59E0B"/></a:accent4>
<a:accent5><a:srgbClr val="8B5CF6"/></a:accent5>
<a:accent6><a:srgbClr val="EF4444"/></a:accent6>
<a:hlink><a:srgbClr val="2F6FEB"/></a:hlink>
<a:folHlink><a:srgbClr val="6B7280"/></a:folHlink>
</a:clrScheme>
<a:fontScheme name="OnClaw">
<a:majorFont><a:latin typeface="Inter"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>
<a:minorFont><a:latin typeface="Inter"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont>
</a:fontScheme>
<a:fmtScheme name="OnClaw">
<a:fillStyleLst>
<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>
<a:solidFill><a:schemeClr val="phClr"><a:tint val="60000"/></a:schemeClr></a:solidFill>
<a:solidFill><a:schemeClr val="phClr"><a:tint val="80000"/></a:schemeClr></a:solidFill>
</a:fillStyleLst>
<a:lnStyleLst>
<a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>
<a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>
<a:ln w="19050"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>
</a:lnStyleLst>
<a:effectStyleLst>
<a:effectStyle><a:effectLst/></a:effectStyle>
<a:effectStyle><a:effectLst/></a:effectStyle>
<a:effectStyle><a:effectLst/></a:effectStyle>
</a:effectStyleLst>
<a:bgFillStyleLst>
<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>
<a:solidFill><a:schemeClr val="phClr"><a:tint val="60000"/></a:schemeClr></a:solidFill>
<a:solidFill><a:schemeClr val="phClr"><a:tint val="80000"/></a:schemeClr></a:solidFill>
</a:bgFillStyleLst>
</a:fmtScheme>
</a:themeElements>
</a:theme>`

// pptxSlideXML renders one slide part: a title placeholder shape and a body
// placeholder shape with one paragraph per bullet (explicit bullet characters
// so bullets render without a list-style chain).
func pptxSlideXML(title string, bullets []string) string {
	var titlePara strings.Builder
	if title == "" {
		titlePara.WriteString(`<a:p><a:endParaRPr lang="en-US"/></a:p>`)
	} else {
		titlePara.WriteString(`<a:p><a:r><a:rPr lang="en-US" sz="4000" b="1" dirty="0"/><a:t>` + xmlEscapeText(title) + `</a:t></a:r></a:p>`)
	}

	var bodyParas strings.Builder
	if len(bullets) == 0 {
		bodyParas.WriteString(`<a:p><a:endParaRPr lang="en-US"/></a:p>`)
	} else {
		for _, bullet := range bullets {
			bodyParas.WriteString(`<a:p><a:pPr><a:buFont typeface="Arial"/><a:buChar char="•"/></a:pPr>` +
				`<a:r><a:rPr lang="en-US" sz="2000" dirty="0"/><a:t>` + xmlEscapeText(bullet) + `</a:t></a:r></a:p>`)
		}
	}

	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
<p:cSld>
<p:spTree>
<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>
<p:sp>
<p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="838200" y="365125"/><a:ext cx="10515600" cy="1325563"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:lstStyle/>` + titlePara.String() + `</p:txBody>
</p:sp>
<p:sp>
<p:nvSpPr><p:cNvPr id="3" name="Content"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="838200" y="1825625"/><a:ext cx="10515600" cy="4351338"/></a:xfrm></p:spPr>
<p:txBody><a:bodyPr/><a:lstStyle/>` + bodyParas.String() + `</p:txBody>
</p:sp>
</p:spTree>
</p:cSld>
<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>
</p:sld>`
}

const pptxSlideRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
</Relationships>`

// buildPPTXZip assembles a full pptx package around the skeleton: static
// skeleton parts plus one generated slideN.xml per input slide, registered in
// presentation.xml, its rels, and the content types.
func buildPPTXZip(slides []pptxSlideInput) []zipEntry {
	entries := make([]zipEntry, 0, len(slides)+8)

	var ct strings.Builder
	ct.WriteString(pptxContentTypesHeader)
	for i := range slides {
		ct.WriteString(fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i+1))
	}
	ct.WriteString(pptxContentTypesFooter)
	entries = append(entries, zipEntry{Name: "[Content_Types].xml", Data: []byte(ct.String())})
	entries = append(entries, zipEntry{Name: "_rels/.rels", Data: []byte(pptxRootRels)})
	entries = append(entries, zipEntry{Name: "ppt/presentation.xml", Data: []byte(pptxPresentationXML(len(slides)))})
	entries = append(entries, zipEntry{Name: "ppt/_rels/presentation.xml.rels", Data: []byte(pptxPresentationRels(len(slides)))})
	entries = append(entries, zipEntry{Name: "ppt/slideMasters/slideMaster1.xml", Data: []byte(pptxSlideMasterXML)})
	entries = append(entries, zipEntry{Name: "ppt/slideMasters/_rels/slideMaster1.xml.rels", Data: []byte(pptxSlideMasterRels)})
	entries = append(entries, zipEntry{Name: "ppt/slideLayouts/slideLayout1.xml", Data: []byte(pptxSlideLayoutXML)})
	entries = append(entries, zipEntry{Name: "ppt/slideLayouts/_rels/slideLayout1.xml.rels", Data: []byte(pptxSlideLayoutRels)})
	entries = append(entries, zipEntry{Name: "ppt/theme/theme1.xml", Data: []byte(pptxThemeXML)})

	for i, slide := range slides {
		n := strconv.Itoa(i + 1)
		entries = append(entries, zipEntry{Name: "ppt/slides/slide" + n + ".xml", Data: []byte(pptxSlideXML(slide.Title, slide.Bullets))})
		entries = append(entries, zipEntry{Name: "ppt/slides/_rels/slide" + n + ".xml.rels", Data: []byte(pptxSlideRels)})
	}
	return entries
}

// pptxSlidePartRe matches the slide parts the template overlay fills.
var pptxSlidePartRe = regexp.MustCompile(`^ppt/slides/slide[0-9]+\.xml$`)
