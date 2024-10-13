// Copyright (c) 2022 Blacknon. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

/*
list package creates a TUI list based on the contents specified in a structure, and returns the selected row.
*/

package list

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/blacknon/lssh/conf"
	termbox "github.com/nsf/termbox-go"
)

// TODO(blacknon):
//     - 外部のライブラリとして外出しする
//     - tomlやjsonなどを渡して、出力項目を指定できるようにする
//     - 指定した項目だけでの検索などができるようにする
//     - 検索方法の充実化(regexでの検索など)
//     - 内部でのウィンドウの実装
//         - 項目について、更新や閲覧ができるようにする
//     - キーバインドの設定変更
//     - Windowsでも動作するように修正する

const (
	DefaultColor           = 255
	DefaultBackgroundColor = 255
)

// ListInfo is Struct at view list.
type ListInfo struct {
	// Incremental search line prompt string
	Prompt string

	NameList   []string
	SelectName []string
	DataList   conf.Config // original config data(struct)
	DataText   []string    // all data text list
	ViewText   []ListItem  // filtered text list
	MultiFlag  bool        // multi select flag
	Keyword    string      // input keyword
	CursorLine int         // cursor line
	Term       TermInfo
}

type ListItem struct {
	Text           string
	MatchedIndexes []int
	Score          int
}

type TermInfo struct {
	Headline        int
	LeftMargin      int
	Color           int
	BackgroundColor int
}

type ListArrayInfo struct {
	Name    string
	Connect string
	Note    string
}

func NewListInfo(prompt string, nameList []string, dataList conf.Config, isMulti bool) *ListInfo {
	return &ListInfo{
		Prompt:    prompt,
		NameList:  nameList,
		DataList:  dataList,
		MultiFlag: isMulti,
		Term: TermInfo{
			Headline:        2,
			LeftMargin:      2,
			Color:           DefaultColor,
			BackgroundColor: DefaultBackgroundColor,
		},
	}
}

// arrayContains returns that arr contains str.
func arrayContains(arr []string, str string) bool {
	for _, v := range arr {
		if v == str {
			return true
		}
	}
	return false
}

// Toggle the selected state of cursor line.
func (l *ListInfo) toggle(newLine string) {
	tmpList := []string{}

	addFlag := true
	for _, selectedLine := range l.SelectName {
		if selectedLine != newLine {
			tmpList = append(tmpList, selectedLine)
		} else {
			addFlag = false
		}
	}
	if addFlag {
		tmpList = append(tmpList, newLine)
	}
	l.SelectName = []string{}
	l.SelectName = tmpList
}

// Toggle the selected state of the currently displayed list
func (l *ListInfo) allToggle(allFlag bool) {
	SelectedList := []string{}
	// selectedList in allSelectedList
	SelectedList = append(SelectedList, l.SelectName...)

	// allFlag is False
	if !allFlag {
		// On each lines that except a header line and are not selected line,
		// toggles left end fields
		for _, addLine := range l.ViewText[1:] {
			addName := strings.Fields(addLine.Text)[0]
			if !arrayContains(SelectedList, addName) {
				l.toggle(addName)
			}
		}
		return
	} else {
		// On each lines that except a header line, toggles left end fields
		for _, addLine := range l.ViewText[1:] {
			addName := strings.Fields(addLine.Text)[0]
			l.toggle(addName)
		}
		return
	}
}

// getText is create view text (use text/tabwriter)
func (l *ListInfo) getText() {
	buffer := &bytes.Buffer{}
	tabWriterBuffer := new(tabwriter.Writer)
	tabWriterBuffer.Init(buffer, 0, 4, 8, ' ', 0)
	fmt.Fprintln(tabWriterBuffer, "ServerName \tConnect Information \tNote \t")

	// Create list table
	for _, key := range l.NameList {
		name := convNewline(key, "")
		conInfo := convNewline(l.DataList.Server[key].User+"@"+l.DataList.Server[key].Addr, "")
		note := convNewline(l.DataList.Server[key].Note, "")

		fmt.Fprintln(tabWriterBuffer, name+"\t"+conInfo+"\t"+note)
	}

	tabWriterBuffer.Flush()
	line, err := buffer.ReadString('\n')
	for err == nil {
		line = convNewline(line, "")

		str := strings.ReplaceAll(line, "\t", " ")
		l.DataText = append(l.DataText, str)
		line, err = buffer.ReadString('\n')
	}
}

// upateFilterText updates l.ViewText with matching keyword (ignore case).
// DataText sets ViewText if keyword is empty.
func (l *ListInfo) updateFilterText() {
	// Initialization ViewText
	l.ViewText = []ListItem{}

	// SearchText Bounds Space
	allServerText := l.DataText[1:] // remove header line
	tmpText := []ListItem{}

	keyword := l.Keyword
	// if No words
	if len(keyword) == 0 {
		for _, text := range l.DataText {
			l.ViewText = append(l.ViewText, ListItem{Text: text})
		}
		return
	}

	l.ViewText = append(l.ViewText, ListItem{Text: l.DataText[0]})
	lowKeyword := regexp.QuoteMeta(strings.ToLower(keyword))
	re := regexp.MustCompile(lowKeyword)
	tmpText = []ListItem{}

	for j := 0; j < len(allServerText); j++ {
		line := allServerText[j]
		if re.MatchString(strings.ToLower(line)) {
			tmpText = append(tmpText, ListItem{Text: line})
		}
	}
	l.ViewText = append(l.ViewText, tmpText...)
}

// View is display the list in TUI
func (l *ListInfo) View() {
	l.getText()
	if len(l.DataText) == 1 {
		return
	}

	if err := termbox.Init(); err != nil {
		panic(err)
	}
	defer termbox.Close()

	// enable termbox mouse input
	termbox.SetInputMode(termbox.InputMouse)

	l.keyEvent()
}

// convNewline is newline replace to nlcode
func convNewline(str, nlcode string) string {
	return strings.NewReplacer(
		"\r\n", nlcode,
		"\r", nlcode,
		"\n", nlcode,
	).Replace(str)
}
