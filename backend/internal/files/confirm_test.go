package files

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectFormat_PDF(t *testing.T) {
	ct, err := detectFormat([]byte("%PDF-1.4\n%…\n1 0 obj"))
	require.NoError(t, err)
	require.Equal(t, contentTypePDF, ct)
}

func TestDetectFormat_XML(t *testing.T) {
	ct, err := detectFormat([]byte(`<?xml version="1.0" encoding="UTF-8"?><root/>`))
	require.NoError(t, err)
	require.Equal(t, contentTypeXML, ct)
}

func TestDetectFormat_XML_WithBOM(t *testing.T) {
	data := append(append([]byte{}, utf8BOM...), []byte(`<?xml version="1.0"?><root/>`)...)
	ct, err := detectFormat(data)
	require.NoError(t, err)
	require.Equal(t, contentTypeXML, ct)
}

func TestDetectFormat_XML_LeadingWhitespace(t *testing.T) {
	ct, err := detectFormat([]byte("  \n\t<?xml version=\"1.0\"?><root/>"))
	require.NoError(t, err)
	require.Equal(t, contentTypeXML, ct)
}

func TestDetectFormat_ZipWithoutDocumentXML(t *testing.T) {
	// PK-сигнатура, но это не валидный zip (или не docx) — magic bytes должны отклонить.
	_, err := detectFormat([]byte("PK\x03\x04not a real zip"))
	require.Error(t, err)
}

func TestDetectFormat_Unknown(t *testing.T) {
	_, err := detectFormat([]byte("случайные байты, не документ"))
	require.Error(t, err)
}

func TestCountPDFPages_RealFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.pdf")
	require.NoError(t, err)

	n, err := countPDFPages(data)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestCountPDFPages_MalformedDoesNotPanic(t *testing.T) {
	// Битый xref у pdfcpu v0.11.0 паникует внутри библиотеки вместо возврата ошибки — countPDFPages
	// обязан перехватить панику и вернуть обычную ошибку (иначе confirm уронит весь запрос вместо
	// REJECTED_CORRUPTED, ТЗ 9.1: "битые файлы отклонять").
	malformed := []byte("%PDF-1.1\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /Resources << >> /MediaBox [0 0 300 144] >>\nendobj\n" +
		"trailer\n<< /Size 4 /Root 1 0 R >>\n")

	require.NotPanics(t, func() {
		_, err := countPDFPages(malformed)
		require.Error(t, err)
	})
}
