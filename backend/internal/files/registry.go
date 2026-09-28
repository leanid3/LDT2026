package files

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Канонические заголовки реестра (CSV/XLSX), общие для всех трёх форматов (backend-plan.md §8.2):
// object_id, file_name, sha256, doc_stage, discipline, document_code, revision, approval_status,
// approval_date, sheet_page_range, predecessor_ref, successor_ref, signature_status — см. rowFromRecord.

// ParseRegistry определяет формат по расширению файла и разбирает в строки реестра.
func ParseRegistry(filename string, r io.Reader) ([]RegistryRow, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv":
		return ParseRegistryCSV(r)
	case ".json":
		return ParseRegistryJSON(r)
	case ".xlsx":
		return ParseRegistryXLSX(r)
	default:
		return nil, fmt.Errorf("registry: неподдерживаемый формат файла %q (ожидается csv/json/xlsx)", filename)
	}
}

func ParseRegistryCSV(r io.Reader) ([]RegistryRow, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("registry csv: read: %w", err)
	}
	// Excel («CSV UTF-8») дописывает BOM, а русская локаль разделяет колонки точкой с запятой:
	// без этого первая колонка называется "\ufeffobject_id", а вся строка читается как одна колонка.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	cr := csv.NewReader(bytes.NewReader(data))
	cr.Comma = detectDelimiter(data)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1 // строки разной длины (хвостовые пустые колонки) — не ошибка
	cr.LazyQuotes = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("registry csv: read header: %w", err)
	}
	idx := headerIndex(header)

	var rows []RegistryRow
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("registry csv: read row: %w", err)
		}
		if row := rowFromRecord(record, idx); !row.isEmpty() {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// detectDelimiter выбирает ';' или ',' по первой строке (заголовку).
func detectDelimiter(data []byte) rune {
	line := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line = data[:i]
	}
	if bytes.Count(line, []byte(";")) > bytes.Count(line, []byte(",")) {
		return ';'
	}
	return ','
}

func ParseRegistryXLSX(r io.Reader) ([]RegistryRow, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("registry xlsx: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("registry xlsx: нет листов")
	}

	all, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("registry xlsx: read rows: %w", err)
	}
	if len(all) == 0 {
		return nil, nil
	}

	idx := headerIndex(all[0])
	rows := make([]RegistryRow, 0, len(all)-1)
	for _, record := range all[1:] {
		if row := rowFromRecord(record, idx); !row.isEmpty() {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// registryJSONRow — JSON-представление строки реестра, поля 1:1 с registryHeaders.
type registryJSONRow struct {
	ObjectID        string `json:"object_id"`
	FileName        string `json:"file_name"`
	SHA256          string `json:"sha256"`
	DocStage        string `json:"doc_stage"`
	Discipline      string `json:"discipline"`
	DocumentCode    string `json:"document_code"`
	Revision        string `json:"revision"`
	ApprovalStatus  string `json:"approval_status"`
	ApprovalDate    string `json:"approval_date"`
	SheetPageRange  string `json:"sheet_page_range"`
	PredecessorRef  string `json:"predecessor_ref"`
	SuccessorRef    string `json:"successor_ref"`
	SignatureStatus string `json:"signature_status"`
}

func ParseRegistryJSON(r io.Reader) ([]RegistryRow, error) {
	var raw []registryJSONRow
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("registry json: decode: %w", err)
	}

	rows := make([]RegistryRow, 0, len(raw))
	for _, j := range raw {
		rows = append(rows, RegistryRow(j))
	}
	return rows, nil
}

func headerIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		key := canonicalHeader(h)
		if _, dup := idx[key]; !dup { // при двух колонках с одним смыслом берём левую
			idx[key] = i
		}
	}
	return idx
}

func rowFromRecord(record []string, idx map[string]int) RegistryRow {
	get := func(col string) string {
		i, ok := idx[col]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}
	return RegistryRow{
		ObjectID: get("object_id"), FileName: get("file_name"), SHA256: get("sha256"),
		DocStage: get("doc_stage"), Discipline: get("discipline"), DocumentCode: get("document_code"),
		Revision: get("revision"), ApprovalStatus: get("approval_status"), ApprovalDate: get("approval_date"),
		SheetPageRange: get("sheet_page_range"), PredecessorRef: get("predecessor_ref"),
		SuccessorRef: get("successor_ref"), SignatureStatus: get("signature_status"),
	}
}

// isEmpty — строка без единого значения (пустая строка таблицы под данными).
func (r RegistryRow) isEmpty() bool {
	return strings.TrimSpace(r.ObjectID+r.FileName+r.SHA256+r.DocStage+r.Discipline+r.DocumentCode+r.Revision+
		r.ApprovalStatus+r.ApprovalDate+r.SheetPageRange+r.PredecessorRef+r.SuccessorRef+r.SignatureStatus) == ""
}
