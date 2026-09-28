package files_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func sp(s string) *string { return &s }

// ---- нормализация значений реестра ----

func TestNormalizeRegistryRow_Values(t *testing.T) {
	good := files.RegistryRow{FileName: "a.pdf", SHA256: strings.Repeat("AB", 32), DocStage: " пд ", Discipline: "АР",
		DocumentCode: "АР-001", Revision: "2", ApprovalStatus: "в производство работ", ApprovalDate: "15.03.2026"}
	got, issues := files.NormalizeRegistryRow(good)
	require.Empty(t, issues)
	require.Equal(t, "PD", got.DocStage, "русская аббревиатура приводится к канонической")
	require.Equal(t, "FOR_CONSTRUCTION", got.ApprovalStatus)
	require.Equal(t, "2026-03-15", got.ApprovalDate)
	require.Equal(t, strings.Repeat("ab", 32), got.SHA256, "sha256 — в нижнем регистре")

	for in, want := range map[string]string{"Утверждено": "APPROVED", "approved": "APPROVED", "for construction": "FOR_CONSTRUCTION",
		"Черновик": "DRAFT", "заменено": "SUPERSEDED", "аннулировано": "CANCELLED"} {
		r := good
		r.ApprovalStatus = in
		out, is := files.NormalizeRegistryRow(r)
		require.Empty(t, is, in)
		require.Equal(t, want, out.ApprovalStatus, in)
	}
	for in, want := range map[string]string{"2026-03-15": "2026-03-15", "15.03.2026": "2026-03-15", "5.3.2026": "2026-03-05", "15/03/2026": "2026-03-15"} {
		r := good
		r.ApprovalDate = in
		out, is := files.NormalizeRegistryRow(r)
		require.Empty(t, is, in)
		require.Equal(t, want, out.ApprovalDate, in)
	}
}

func TestNormalizeRegistryRow_FatalAndWarnings(t *testing.T) {
	base := files.RegistryRow{FileName: "a.pdf", DocStage: "PD", Discipline: "АР", DocumentCode: "К-1", Revision: "1", ApprovalStatus: "APPROVED", ApprovalDate: "2026-01-01"}
	fatalFields := func(r files.RegistryRow) (fatal, warn []string) {
		_, issues := files.NormalizeRegistryRow(r)
		for _, i := range issues {
			if i.Fatal {
				fatal = append(fatal, i.Field)
			} else {
				warn = append(warn, i.Field)
			}
		}
		return
	}

	for name, mutate := range map[string]func(*files.RegistryRow){
		"doc_stage": func(r *files.RegistryRow) { r.DocStage = "XX" }, "doc_stage-empty": func(r *files.RegistryRow) { r.DocStage = "" },
		"approval_status": func(r *files.RegistryRow) { r.ApprovalStatus = "почти утверждено" },
		"approval_date":   func(r *files.RegistryRow) { r.ApprovalDate = "вчера" },
		"file_name":       func(r *files.RegistryRow) { r.FileName = ""; r.SHA256 = "" },
	} {
		r := base
		mutate(&r)
		f, _ := fatalFields(r)
		require.Equal(t, []string{strings.Split(name, "-")[0]}, f, name)
	}

	// Пустые «желательные» поля — предупреждения, строка остаётся применимой.
	r := base
	r.ApprovalStatus, r.ApprovalDate, r.Revision, r.DocumentCode, r.Discipline = "", "", "", "", ""
	f, w := fatalFields(r)
	require.Empty(t, f)
	require.ElementsMatch(t, []string{"approval_status", "approval_date", "revision", "document_code", "discipline"}, w)

	// Битый sha256 не блокирует строку: сопоставление по имени, предупреждение.
	r = base
	r.SHA256 = "12345"
	out, _ := files.NormalizeRegistryRow(r)
	f, w = fatalFields(r)
	require.Empty(t, f)
	require.Equal(t, []string{"sha256"}, w)
	require.Empty(t, out.SHA256)
}

// ---- CSV, как его отдаёт Excel ----

func TestParseRegistryCSV_ExcelFlavours(t *testing.T) {
	semicolon := "\ufeffobject_id;file_name;sha256;doc_stage;approval_status\r\n" +
		"o1;a.pdf;" + strings.Repeat("a", 64) + ";ПД;утверждено\r\n" +
		";;;;\r\n" + // пустая строка под данными
		"o1;b.pdf;;РД;\r\n"
	rows, err := files.ParseRegistryCSV(strings.NewReader(semicolon))
	require.NoError(t, err)
	require.Len(t, rows, 2, "пустая строка пропущена")
	require.Equal(t, "o1", rows[0].ObjectID, "BOM не портит имя первой колонки")
	require.Equal(t, "ПД", rows[0].DocStage)
	require.Equal(t, "b.pdf", rows[1].FileName)

	russian := "Имя файла,Стадия,Шифр документа,Редакция,Статус утверждения,Дата утверждения\n" +
		"a.pdf,PD,АР-1,2,APPROVED,01.02.2026\n"
	rows, err = files.ParseRegistryCSV(strings.NewReader(russian))
	require.NoError(t, err)
	require.Equal(t, files.RegistryRow{FileName: "a.pdf", DocStage: "PD", DocumentCode: "АР-1", Revision: "2", ApprovalStatus: "APPROVED", ApprovalDate: "01.02.2026"}, rows[0],
		"русские названия колонок понимаются")
}

// ---- шаблон: то, что мы выдаём, должно читаться тем, что мы принимаем ----

func sampleFiles() []files.File {
	first, second := uuid.New(), uuid.New()
	date := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	return []files.File{
		{ID: first, OriginalName: "АР-001_ред1.pdf", SHA256: sp(strings.Repeat("a", 64)), CheckStatus: "ACCEPTED", SuccessorID: &second,
			DocStage: sp("PD"), Discipline: sp("АР"), DocumentCode: sp("АР-001"), Revision: sp("1"), ApprovalStatus: sp("SUPERSEDED")},
		{ID: second, OriginalName: "АР-001_ред2.pdf", SHA256: sp(strings.Repeat("b", 64)), CheckStatus: "ACCEPTED", PredecessorID: &first,
			DocStage: sp("PD"), Discipline: sp("АР"), DocumentCode: sp("АР-001"), Revision: sp("2"), ApprovalStatus: sp("APPROVED"), ApprovalDate: &date, SheetPageRange: sp("1-12")},
		{ID: uuid.New(), OriginalName: "вирус.pdf", SHA256: sp(strings.Repeat("c", 64)), CheckStatus: "REJECTED_VIRUS"},
		{ID: uuid.New(), OriginalName: "не-дозагружен.pdf", CheckStatus: "UPLOADING"},
	}
}

func TestRegistryTemplate_XLSX_RoundTripsThroughParser(t *testing.T) {
	tpl, err := files.BuildRegistryTemplate("xlsx", "obj-1", sampleFiles())
	require.NoError(t, err)
	require.Equal(t, "registry-template.xlsx", tpl.Filename)
	require.Contains(t, tpl.ContentType, "spreadsheetml")

	rows, err := files.ParseRegistryXLSX(bytes.NewReader(tpl.Data))
	require.NoError(t, err)
	require.Len(t, rows, 2, "в реестр попадают только принятые файлы")
	require.Equal(t, "obj-1", rows[0].ObjectID)
	require.Equal(t, "АР-001_ред1.pdf", rows[0].FileName)
	require.Equal(t, strings.Repeat("a", 64), rows[0].SHA256)
	require.Equal(t, "АР-001_ред2.pdf", rows[0].SuccessorRef, "связи подставлены именами файлов")
	require.Equal(t, "АР-001_ред1.pdf", rows[1].PredecessorRef)
	require.Equal(t, "15.03.2026", rows[1].ApprovalDate)
	require.Equal(t, "1-12", rows[1].SheetPageRange, "«1-12» не превратилось в дату")

	// И каждая строка шаблона проходит нашу же валидацию без фатальных замечаний.
	for _, r := range rows {
		_, issues := files.NormalizeRegistryRow(r)
		for _, i := range issues {
			require.False(t, i.Fatal, "%s: %s", i.Field, i.Message)
		}
	}
}

func TestRegistryTemplate_XLSX_StructureAndInstruction(t *testing.T) {
	tpl, err := files.BuildRegistryTemplate("", "", nil) // пустой формат = xlsx, пустой шаблон
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(tpl.Data))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, []string{"Реестр", "Инструкция"}, f.GetSheetList(), "«Реестр» — первый лист: именно его читает парсер")

	header, err := f.GetRows("Реестр")
	require.NoError(t, err)
	require.Equal(t, files.RegistryHeaders(), header[0])
	require.Len(t, header, 1, "пустой шаблон — только заголовок")

	dvs, err := f.GetDataValidations("Реестр")
	require.NoError(t, err)
	lists := map[string]string{}
	for _, dv := range dvs {
		lists[dv.Sqref] = dv.Formula1
	}
	require.Contains(t, lists["D2:D2000"], "PD", "выпадающий список стадий")
	require.Contains(t, lists["H2:H2000"], "FOR_CONSTRUCTION", "выпадающий список статусов")

	instr, err := f.GetRows("Инструкция")
	require.NoError(t, err)
	var flat strings.Builder
	for _, r := range instr {
		flat.WriteString(strings.Join(r, " | ") + "\n")
	}
	text := flat.String()
	for _, spec := range files.RegistryFields {
		require.Contains(t, text, spec.Name, "в инструкции описана колонка %s", spec.Name)
	}
	require.Contains(t, text, "CLARIFICATION_REQUIRED")
	require.Contains(t, text, "predecessor_ref")
}

func TestRegistryTemplate_CSV_RoundTripsAndOpensInExcel(t *testing.T) {
	tpl, err := files.BuildRegistryTemplate("csv", "obj-1", sampleFiles())
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(tpl.Data, []byte("\xef\xbb\xbf")), "BOM: русская версия Excel читает UTF-8")
	require.Contains(t, string(tpl.Data), "object_id;file_name;sha256;", "разделитель «;» — колонки в русском Excel")

	rows, err := files.ParseRegistryCSV(bytes.NewReader(tpl.Data))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "АР-001_ред2.pdf", rows[0].SuccessorRef)
}

func TestRegistryTemplate_UnknownFormat(t *testing.T) {
	_, err := files.BuildRegistryTemplate("pdf", "", nil)
	require.ErrorIs(t, err, files.ErrValidation)
}

// docs/registry.md — инструкция для людей; не должна расходиться с тем, что принимает парсер.
func TestRegistryDoc_MentionsEveryField(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/registry.md")
	require.NoError(t, err, "нужен docs/registry.md с инструкцией по заполнению реестра")
	for _, spec := range files.RegistryFields {
		require.Contains(t, string(doc), spec.Name)
	}
	for _, v := range append(append([]string{}, files.ApprovalStatuses...), files.DocStages...) {
		require.Contains(t, string(doc), v)
	}
}

// ---- загрузка реестра: валидация вместо 500 ----

func TestUploadRegistry_InvalidRowsAreReportedNotFatal_AndDateIsSaved(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	repo := files.NewRepository(database.Pool())
	svc := files.NewService(database, repo, nil, nil, fakeProcessGate{})

	objectID, processID := uuid.New(), uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'o')`, objectID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PENDING')`, processID, objectID)
	require.NoError(t, err)
	ids := map[string]uuid.UUID{}
	for i, name := range []string{"ok.pdf", "bad-stage.pdf", "bad-date.pdf", "nofields.pdf"} {
		id := uuid.New()
		ids[name] = id
		_, err := database.Pool().Exec(ctx, `
			INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, sha256, check_status)
			VALUES ($1, $2, $3, $4, 'k/'||$4, 1, $5, 'ACCEPTED')`, id, objectID, processID, name, strings.Repeat(string(rune('a'+i)), 64))
		require.NoError(t, err)
	}

	csv := "file_name;doc_stage;discipline;document_code;revision;approval_status;approval_date\n" +
		"ok.pdf;ПД;АР;АР-1;1;утверждено;15.03.2026\n" +
		"bad-stage.pdf;проектная;АР;АР-1;2;APPROVED;16.03.2026\n" +
		"bad-date.pdf;RD;АР;АР-2;1;APPROVED;вчера\n" +
		"nofields.pdf;ID;;;;;\n" +
		"ghost.pdf;PD;АР;АР-3;1;APPROVED;01.01.2026\n"

	res, err := svc.UploadRegistry(ctx, processID, "registry.csv", strings.NewReader(csv))
	require.NoError(t, err, "недопустимые значения не должны валить весь реестр (раньше — 500 на CHECK)")

	require.Equal(t, 5, res.TotalRows)
	require.Equal(t, 2, res.Matched, "ok.pdf и nofields.pdf")
	require.Equal(t, 2, res.Invalid, "bad-stage.pdf и bad-date.pdf")
	require.Equal(t, 1, res.Unmatched, "ghost.pdf")

	errText := strings.Join(res.Errors, "\n")
	require.Contains(t, errText, "строка 3, doc_stage")
	require.Contains(t, errText, "строка 4, approval_date")
	require.Contains(t, errText, "ghost.pdf")
	warnText := strings.Join(res.Warnings, "\n")
	require.Contains(t, warnText, "строка 5, approval_status", "nofields.pdf принят, но с предупреждениями о незаполненном")

	ok, err := repo.Get(ctx, ids["ok.pdf"])
	require.NoError(t, err)
	require.Equal(t, "PD", *ok.DocStage)
	require.Equal(t, "APPROVED", *ok.ApprovalStatus)
	require.NotNil(t, ok.ApprovalDate, "approval_date раньше молча терялся")
	require.Equal(t, "2026-03-15", ok.ApprovalDate.Format("2006-01-02"))

	bad, err := repo.Get(ctx, ids["bad-stage.pdf"])
	require.NoError(t, err)
	require.Nil(t, bad.DocStage, "отклонённая строка не применялась")

	var invalidEntries int
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM registry_entries WHERE process_id = $1 AND match_status = 'INVALID'`, processID).Scan(&invalidEntries))
	require.Equal(t, 2, invalidEntries, "отклонённые строки сохранены для аудита")

	// Исправленный реестр можно загрузить повторно — данные обновляются.
	fixed := "file_name;doc_stage;discipline;document_code;revision;approval_status;approval_date\n" +
		"bad-stage.pdf;PD;АР;АР-1;2;APPROVED;16.03.2026\n"
	res, err = svc.UploadRegistry(ctx, processID, "registry.csv", strings.NewReader(fixed))
	require.NoError(t, err)
	require.Equal(t, 1, res.Matched)
	require.Empty(t, res.Errors)
	bad, err = repo.Get(ctx, ids["bad-stage.pdf"])
	require.NoError(t, err)
	require.Equal(t, "PD", *bad.DocStage)
}
