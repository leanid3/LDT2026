package files

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Requirement — насколько поле обязательно для реестра (ТЗ, «Выбор актуальной редакции»: для каждого файла
// обязательны object_id, doc_stage, discipline, document_code, revision, approval_status, approval_date,
// sheet/page, file_hash и связь predecessor/successor).
type Requirement string

const (
	ReqRequired    Requirement = "обязательно" // без него файл не будет принят из реестра
	ReqRecommended Requirement = "желательно"  // строка принимается, но файл не сможет быть эталоном
	ReqOptional    Requirement = "по необходимости"
)

// FieldSpec описывает колонку реестра. Единый источник для валидации, шаблона (XLSX/CSV), листа
// «Инструкция» и docs/registry.md — чтобы описание не расходилось с тем, что принимает парсер.
type FieldSpec struct {
	Name        string // канонический заголовок колонки (латиница, как в парсере)
	Title       string // название по-русски
	Requirement Requirement
	Allowed     []string // допустимые значения; пусто — свободный текст
	Description string
	Example     string
}

// Допустимые значения статуса утверждения (backend-plan.md §4.1).
var ApprovalStatuses = []string{"DRAFT", "APPROVED", "FOR_CONSTRUCTION", "SUPERSEDED", "CANCELLED"}

var DocStages = []string{"PD", "RD", "ID"}

// RegistryFields — колонки реестра в порядке шаблона.
var RegistryFields = []FieldSpec{
	{Name: "object_id", Title: "Объект", Requirement: ReqRequired,
		Description: "Идентификатор объекта (UUID из карточки объекта). В шаблоне, скачанном для проверки, заполнен автоматически.",
		Example:     "3f2b8c1e-5a4d-4c7e-9b1a-2d6e8f0a1b3c"},
	{Name: "file_name", Title: "Имя файла", Requirement: ReqRequired,
		Description: "Имя файла ТОЧНО как при загрузке (с расширением). Нужно имя или sha256; лучше оба.",
		Example:     "АР-001_ПД.pdf"},
	{Name: "sha256", Title: "Контрольная сумма", Requirement: ReqRequired,
		Description: "SHA-256 файла (64 символа). Главный ключ сопоставления, надёжнее имени. В шаблоне для проверки заполнена автоматически — вручную не вычисляйте.",
		Example:     "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
	{Name: "doc_stage", Title: "Стадия", Requirement: ReqRequired, Allowed: DocStages,
		Description: "Стадия документации: PD — проектная, RD — рабочая, ID — исполнительная. Допустимо писать ПД / РД / ИД.",
		Example:     "PD"},
	{Name: "discipline", Title: "Раздел (дисциплина)", Requirement: ReqRecommended,
		Description: "Раздел/марка комплекта: АР, КР, ОВ, ВК, ЭОМ и т.п. Редакции сравниваются только внутри одного раздела.",
		Example:     "АР"},
	{Name: "document_code", Title: "Шифр документа", Requirement: ReqRecommended,
		Description: "Шифр комплекта. У всех редакций одного документа шифр ОДИНАКОВЫЙ — по нему система собирает цепочку редакций.",
		Example:     "АР-001"},
	{Name: "revision", Title: "Редакция", Requirement: ReqRecommended,
		Description: "Номер или обозначение редакции: 1, 2, Изм.3. Знаки не удаляйте.",
		Example:     "2"},
	{Name: "approval_status", Title: "Статус утверждения", Requirement: ReqRecommended, Allowed: ApprovalStatuses,
		Description: "APPROVED — утверждена; FOR_CONSTRUCTION — «в производство работ»; DRAFT — черновик (эталоном быть не может); SUPERSEDED — заменена более новой; CANCELLED — аннулирована. Допустимы русские: утверждено, в производство работ, черновик, заменено, аннулировано.",
		Example:     "APPROVED"},
	{Name: "approval_date", Title: "Дата утверждения", Requirement: ReqRecommended,
		Description: "Дата утверждения редакции: ДД.ММ.ГГГГ или ГГГГ-ММ-ДД.",
		Example:     "15.03.2026"},
	{Name: "sheet_page_range", Title: "Листы / страницы", Requirement: ReqOptional,
		Description: "Какие листы или страницы охватывает файл.",
		Example:     "1-12"},
	{Name: "predecessor_ref", Title: "Предыдущая редакция", Requirement: ReqOptional,
		Description: "sha256 или имя файла предыдущей редакции этого документа. Заполняйте, если редакций несколько, иначе система не поймёт, какая из них актуальна.",
		Example:     "АР-001_ПД_ред1.pdf"},
	{Name: "successor_ref", Title: "Следующая редакция", Requirement: ReqOptional,
		Description: "sha256 или имя файла следующей редакции (достаточно указать либо предыдущую, либо следующую).",
		Example:     ""},
	{Name: "signature_status", Title: "Подпись", Requirement: ReqOptional,
		Description: "Наличие электронной подписи, например SIGNED / UNSIGNED. На выбор редакции пока не влияет.",
		Example:     "SIGNED"},
}

// RegistryHeaders — заголовки колонок шаблона.
func RegistryHeaders() []string {
	out := make([]string, len(RegistryFields))
	for i, f := range RegistryFields {
		out[i] = f.Name
	}
	return out
}

// headerAliases — другие названия колонок, которые встречаются в реальных таблицах (русские названия,
// имена из ТЗ: file_hash, sheet/page). Ключи — в нижнем регистре.
var headerAliases = map[string]string{
	"id объекта": "object_id", "объект": "object_id",
	"имя файла": "file_name", "файл": "file_name", "file": "file_name", "filename": "file_name",
	"хеш": "sha256", "хэш": "sha256", "контрольная сумма": "sha256", "file_hash": "sha256", "hash": "sha256",
	"стадия": "doc_stage", "doc stage": "doc_stage",
	"раздел": "discipline", "дисциплина": "discipline", "раздел (дисциплина)": "discipline",
	"шифр": "document_code", "шифр документа": "document_code",
	"редакция": "revision", "ревизия": "revision",
	"статус утверждения": "approval_status", "статус": "approval_status",
	"дата утверждения": "approval_date",
	"листы":            "sheet_page_range", "лист": "sheet_page_range", "страницы": "sheet_page_range",
	"листы / страницы": "sheet_page_range", "лист/страница": "sheet_page_range", "sheet/page": "sheet_page_range",
	"предыдущая редакция": "predecessor_ref", "предшественник": "predecessor_ref",
	"следующая редакция": "successor_ref", "преемник": "successor_ref",
	"подпись": "signature_status", "статус подписи": "signature_status",
}

// canonicalHeader приводит заголовок колонки к каноническому имени (или возвращает его как есть).
func canonicalHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
	if c, ok := headerAliases[h]; ok {
		return c
	}
	return h
}

// RowIssue — проблема в строке реестра. Fatal — строка не применяется; иначе — предупреждение.
type RowIssue struct {
	Field   string
	Message string
	Fatal   bool
}

var (
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

	stageSynonyms = map[string]string{"ПД": "PD", "РД": "RD", "ИД": "ID"}

	statusSynonyms = map[string]string{
		"УТВЕРЖДЕНО": "APPROVED", "УТВЕРЖДЕНА": "APPROVED", "УТВ": "APPROVED",
		"В ПРОИЗВОДСТВО РАБОТ": "FOR_CONSTRUCTION", "В ПРОИЗВОДСТВО": "FOR_CONSTRUCTION", "ДЛЯ СТРОИТЕЛЬСТВА": "FOR_CONSTRUCTION",
		"ЧЕРНОВИК": "DRAFT",
		"ЗАМЕНЕНО": "SUPERSEDED", "ЗАМЕНЕНА": "SUPERSEDED", "УСТАРЕЛО": "SUPERSEDED", "УСТАРЕЛА": "SUPERSEDED",
		"АННУЛИРОВАНО": "CANCELLED", "АННУЛИРОВАНА": "CANCELLED", "ОТМЕНЕНО": "CANCELLED", "ОТМЕНЕНА": "CANCELLED",
	}

	dateLayouts = []string{"2006-01-02", "02.01.2006", "2.1.2006", "02/01/2006", "2/1/2006", "02.01.06", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05"}
)

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// NormalizeRegistryRow приводит значения строки к каноническим (PD/RD/ID, статусы, дата ISO) и проверяет их.
// Невалидное значение в поле с фиксированным списком — фатально: иначе оно упало бы на CHECK в БД
// (500 на весь реестр) или, хуже, молча дало бы неверную стадию. Пустое «желательное» поле — предупреждение.
func NormalizeRegistryRow(row RegistryRow) (RegistryRow, []RowIssue) {
	var issues []RowIssue
	fatal := func(field, format string, args ...any) {
		issues = append(issues, RowIssue{Field: field, Message: fmt.Sprintf(format, args...), Fatal: true})
	}
	warn := func(field, format string, args ...any) {
		issues = append(issues, RowIssue{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	row.ObjectID, row.FileName = strings.TrimSpace(row.ObjectID), strings.TrimSpace(row.FileName)
	row.Discipline, row.DocumentCode = strings.TrimSpace(row.Discipline), strings.TrimSpace(row.DocumentCode)
	row.Revision, row.SheetPageRange = strings.TrimSpace(row.Revision), strings.TrimSpace(row.SheetPageRange)
	row.PredecessorRef, row.SuccessorRef = strings.TrimSpace(row.PredecessorRef), strings.TrimSpace(row.SuccessorRef)
	row.SignatureStatus = strings.TrimSpace(row.SignatureStatus)

	row.SHA256 = strings.ToLower(strings.TrimSpace(row.SHA256))
	if row.SHA256 != "" && !sha256Re.MatchString(row.SHA256) {
		warn("sha256", "«%s» — не SHA-256 (нужно 64 шестнадцатеричных символа); сопоставление только по имени файла", row.SHA256)
		row.SHA256 = ""
	}
	if row.SHA256 == "" && row.FileName == "" {
		fatal("file_name", "не заполнены ни file_name, ни sha256 — непонятно, к какому файлу относится строка")
	}

	stage := strings.ToUpper(strings.TrimSpace(row.DocStage))
	if syn, ok := stageSynonyms[stage]; ok {
		stage = syn
	}
	switch {
	case stage == "":
		fatal("doc_stage", "не заполнена стадия (допустимо: PD, RD, ID)")
	case !oneOf(stage, DocStages):
		fatal("doc_stage", "«%s» — недопустимая стадия (допустимо: PD, RD, ID или ПД, РД, ИД)", row.DocStage)
	}
	row.DocStage = stage

	status := strings.ToUpper(strings.Join(strings.Fields(row.ApprovalStatus), " "))
	if syn, ok := statusSynonyms[status]; ok {
		status = syn
	}
	status = strings.ReplaceAll(status, " ", "_")
	switch {
	case status == "":
		warn("approval_status", "не заполнен статус утверждения — файл не сможет быть эталоном")
	case !oneOf(status, ApprovalStatuses):
		fatal("approval_status", "«%s» — недопустимый статус (допустимо: %s)", row.ApprovalStatus, strings.Join(ApprovalStatuses, ", "))
	}
	row.ApprovalStatus = status

	if d := strings.TrimSpace(row.ApprovalDate); d == "" {
		warn("approval_date", "не заполнена дата утверждения")
		row.ApprovalDate = ""
	} else if t, ok := parseDate(d); ok {
		row.ApprovalDate = t.Format("2006-01-02")
	} else {
		fatal("approval_date", "«%s» — не дата (ожидается ДД.ММ.ГГГГ или ГГГГ-ММ-ДД)", d)
	}

	for _, req := range []struct{ field, val string }{{"discipline", row.Discipline}, {"document_code", row.DocumentCode}, {"revision", row.Revision}} {
		if req.val == "" {
			warn(req.field, "не заполнено — редакции этого документа не удастся сопоставить друг с другом")
		}
	}
	return row, issues
}

func parseDate(s string) (time.Time, bool) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
