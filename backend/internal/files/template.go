package files

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	TemplateFormatXLSX = "xlsx"
	TemplateFormatCSV  = "csv"

	registrySheet    = "Реестр"
	instructionSheet = "Инструкция"
	templateMaxRows  = 2000 // сколько строк покрывают выпадающие списки и текстовый формат
)

// TemplateFile — готовый к отдаче файл шаблона реестра.
type TemplateFile struct {
	Filename    string
	ContentType string
	Data        []byte
}

// BuildRegistryTemplate строит шаблон реестра. Если переданы файлы процесса — строки предзаполнены
// (объект, имя, sha256, а для уже загруженного ранее реестра — и остальные поля, чтобы можно было
// поправить и загрузить снова). Первый лист XLSX — «Реестр»: именно его читает парсер.
func BuildRegistryTemplate(format, objectID string, files []File) (TemplateFile, error) {
	rows := templateRows(objectID, files)
	switch format {
	case TemplateFormatCSV:
		data, err := buildCSV(rows)
		if err != nil {
			return TemplateFile{}, err
		}
		return TemplateFile{Filename: "registry-template.csv", ContentType: "text/csv; charset=utf-8", Data: data}, nil
	case TemplateFormatXLSX, "":
		data, err := buildXLSX(rows)
		if err != nil {
			return TemplateFile{}, err
		}
		return TemplateFile{
			Filename:    "registry-template.xlsx",
			ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			Data:        data,
		}, nil
	default:
		return TemplateFile{}, fmt.Errorf("%w: формат шаблона %q (ожидается xlsx или csv)", ErrValidation, format)
	}
}

// templateRows — строки данных под заголовком (в порядке RegistryFields).
func templateRows(objectID string, files []File) [][]string {
	nameByID := make(map[string]string, len(files))
	for _, f := range files {
		nameByID[f.ID.String()] = f.OriginalName
	}
	ref := func(id fmt.Stringer, ok bool) string {
		if !ok {
			return ""
		}
		return nameByID[id.String()]
	}

	rows := make([][]string, 0, len(files))
	for _, f := range files {
		if f.CheckStatus != CheckStatusAccepted {
			continue // отклонённые/непроверенные в реестр не попадают
		}
		row := []string{objectID, f.OriginalName, deref(f.SHA256), deref(f.DocStage), deref(f.Discipline),
			deref(f.DocumentCode), deref(f.Revision), deref(f.ApprovalStatus), "", deref(f.SheetPageRange), "", "", deref(f.SignatureStatus)}
		if f.ApprovalDate != nil {
			row[8] = f.ApprovalDate.Format("02.01.2006")
		}
		if f.PredecessorID != nil {
			row[10] = ref(f.PredecessorID, true)
		}
		if f.SuccessorID != nil {
			row[11] = ref(f.SuccessorID, true)
		}
		rows = append(rows, row)
	}
	return rows
}

// buildCSV: UTF-8 с BOM и разделителем «;» — так файл корректно открывается в русской версии Excel
// двойным щелчком. Парсер понимает и «;», и «,», и BOM.
func buildCSV(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	if err := w.Write(RegistryHeaders()); err != nil {
		return nil, err
	}
	if err := w.WriteAll(rows); err != nil { // WriteAll делает Flush
		return nil, err
	}
	return buf.Bytes(), w.Error()
}

func buildXLSX(rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", registrySheet); err != nil {
		return nil, err
	}

	header, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"2F5597"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return nil, err
	}
	// Текстовый формат колонок: иначе Excel превратит «1-12» в дату, «1.10» в 1,1, а дату — в число.
	text, err := f.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		return nil, err
	}

	last, _ := excelize.ColumnNumberToName(len(RegistryFields))
	if err := f.SetColStyle(registrySheet, "A:"+last, text); err != nil {
		return nil, err
	}
	for i, spec := range RegistryFields {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetCellValue(registrySheet, col+"1", spec.Name); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(registrySheet, col+"1", col+"1", header); err != nil {
			return nil, err
		}
		width := 18.0
		switch spec.Name {
		case "object_id", "sha256":
			width = 40
		case "file_name":
			width = 32
		case "predecessor_ref", "successor_ref":
			width = 28
		}
		if err := f.SetColWidth(registrySheet, col, col, width); err != nil {
			return nil, err
		}
		// Подсказка при наведении на заголовок и выпадающий список там, где значения фиксированы.
		if len(spec.Allowed) > 0 {
			dv := excelize.NewDataValidation(true)
			dv.Sqref = fmt.Sprintf("%s2:%s%d", col, col, templateMaxRows)
			if err := dv.SetDropList(spec.Allowed); err != nil {
				return nil, err
			}
			dv.SetError(excelize.DataValidationErrorStyleStop, "Недопустимое значение",
				"Допустимо: "+strings.Join(spec.Allowed, ", "))
			if err := f.AddDataValidation(registrySheet, dv); err != nil {
				return nil, err
			}
		}
	}
	if err := f.SetRowHeight(registrySheet, 1, 24); err != nil {
		return nil, err
	}
	if err := f.SetPanes(registrySheet, &excelize.Panes{Freeze: true, Split: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, err
	}

	for r, row := range rows {
		for c, v := range row {
			if v == "" {
				continue
			}
			col, _ := excelize.ColumnNumberToName(c + 1)
			// SetCellStr: значение остаётся строкой (sha256 из цифр не превратится в число).
			if err := f.SetCellStr(registrySheet, fmt.Sprintf("%s%d", col, r+2), v); err != nil {
				return nil, err
			}
		}
	}

	if err := writeInstructionSheet(f); err != nil {
		return nil, err
	}
	f.SetActiveSheet(0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeInstructionSheet(f *excelize.File) error {
	if _, err := f.NewSheet(instructionSheet); err != nil {
		return err
	}
	title, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	if err != nil {
		return err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	head, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"D9E2F3"}, Pattern: 1},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"},
	})
	if err != nil {
		return err
	}
	wrap, err := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	if err != nil {
		return err
	}

	row := 1
	put := func(col string, v string, style int) {
		cell := fmt.Sprintf("%s%d", col, row)
		_ = f.SetCellStr(instructionSheet, cell, v)
		if style != 0 {
			_ = f.SetCellStyle(instructionSheet, cell, cell, style)
		}
	}
	line := func(text string, style int) {
		put("A", text, style)
		row++
	}

	line("Как заполнить реестр файлов", title)
	row++
	steps := []string{
		"1. Заполните лист «Реестр»: одна строка — один загруженный файл. Первую строку (заголовки) не меняйте и не удаляйте.",
		"2. Если шаблон скачан для конкретной проверки, колонки object_id, file_name и sha256 уже заполнены — не правьте их.",
		"3. Заполните стадию (PD / RD / ID), раздел, шифр, редакцию, статус и дату утверждения. В колонках со списком выбирайте значение из выпадающего списка.",
		"4. Если у документа несколько редакций — укажите предыдущую (predecessor_ref) или следующую (successor_ref): имя файла или sha256.",
		"5. Сохраните как .xlsx (или .csv) и загрузите на шаге «Реестр». Ответ покажет, сколько строк принято, какие отклонены и почему.",
		"6. Ошибочные строки не применяются, остальные принимаются. Исправьте и загрузите реестр ещё раз — это безопасно, данные обновятся.",
	}
	for _, s := range steps {
		line(s, 0)
	}
	row++

	line("Колонки", bold)
	for i, h := range []string{"Колонка", "Название", "Заполнение", "Допустимые значения", "Описание", "Пример"} {
		col, _ := excelize.ColumnNumberToName(i + 1)
		put(col, h, head)
	}
	row++
	for _, spec := range RegistryFields {
		vals := []string{spec.Name, spec.Title, string(spec.Requirement), strings.Join(spec.Allowed, ", "), spec.Description, spec.Example}
		for i, v := range vals {
			col, _ := excelize.ColumnNumberToName(i + 1)
			put(col, v, wrap)
		}
		row++
	}
	row++

	line("Как система выбирает актуальную редакцию", bold)
	for _, s := range []string{
		"Файлы одного раздела с одинаковым шифром образуют цепочку редакций (объект + стадия + раздел + шифр).",
		"Эталоном может быть только редакция со статусом APPROVED или FOR_CONSTRUCTION, у которой нет более новой утверждённой редакции.",
		"DRAFT эталоном не бывает; SUPERSEDED и CANCELLED исключаются.",
		"Если актуальную редакцию определить нельзя (две утверждённые без связи, цикл ссылок, ссылка на несуществующий файл) — статус CLARIFICATION_REQUIRED: такой файл не используется как эталон, пока вы не уточните реестр.",
		"Без реестра стадии у файлов неизвестны, и проверка ничего не сравнит.",
	} {
		line("• "+s, 0)
	}
	row++

	line("Пример цепочки из двух редакций", bold)
	ex := [][]string{
		{"file_name", "doc_stage", "discipline", "document_code", "revision", "approval_status", "approval_date", "predecessor_ref"},
		{"АР-001_ред1.pdf", "PD", "АР", "АР-001", "1", "SUPERSEDED", "10.01.2026", ""},
		{"АР-001_ред2.pdf", "PD", "АР", "АР-001", "2", "APPROVED", "15.03.2026", "АР-001_ред1.pdf"},
	}
	for i, r := range ex {
		for c, v := range r {
			col, _ := excelize.ColumnNumberToName(c + 1)
			if i == 0 {
				put(col, v, head)
			} else {
				put(col, v, wrap)
			}
		}
		row++
	}

	for col, w := range map[string]float64{"A": 26, "B": 22, "C": 18, "D": 34, "E": 70, "F": 34} {
		if err := f.SetColWidth(instructionSheet, col, col, w); err != nil {
			return err
		}
	}
	return nil
}
