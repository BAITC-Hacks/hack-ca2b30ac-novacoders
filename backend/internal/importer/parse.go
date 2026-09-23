package importer

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"
)

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(s, "ё", "е")), " "))
}

func number(s string) (float64, bool, error) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return 0, false, nil
	}
	s = strings.ReplaceAll(s, ",", ".")
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = "-" + s[1:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e12 {
		return 0, false, fmt.Errorf("некорректное число")
	}
	return v, true, nil
}

var monthNames = []string{"январ", "феврал", "март", "апрел", "май", "июн", "июл", "август", "сентябр", "октябр", "ноябр", "декабр"}
var englishMonths = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
var russianShortMonths = []string{"янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
var yearPattern = regexp.MustCompile(`(?:^|[^0-9])(20[0-9]{2})(?:[^0-9]|$)`)
var yearMonthPattern = regexp.MustCompile(`^(20[0-9]{2})[-./](0?[1-9]|1[0-2])$`)
var monthYearPattern = regexp.MustCompile(`^(0?[1-9]|1[0-2])[-./](20[0-9]{2})$`)

func monthNumber(s string) int {
	s = normalize(s)
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 12 {
		return n
	}
	for i, name := range monthNames {
		if strings.Contains(s, name) || (i == 4 && strings.Contains(s, "мая")) || strings.HasPrefix(s, englishMonths[i]) || strings.HasPrefix(s, russianShortMonths[i]) {
			return i + 1
		}
	}
	return 0
}

func monthKey(s string, yearHint int, date1904 bool) string {
	s = normalize(s)
	if m := yearMonthPattern.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[2])
		return fmt.Sprintf("%s-%02d", m[1], n)
	}
	if m := monthYearPattern.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return fmt.Sprintf("%s-%02d", m[2], n)
	}
	for _, layout := range []string{"02.01.2006", "2.1.2006", "2006-01-02", "1/2/06", "01/02/2006", "Jan-06", "Jan-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01")
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil && v > 30000 && v < 100000 {
		if t, err := excelize.ExcelDateToTime(v, date1904); err == nil {
			return t.Format("2006-01")
		}
	}
	year := yearHint
	if match := yearPattern.FindStringSubmatch(s); match != nil {
		year, _ = strconv.Atoi(match[1])
	}
	if month := monthNumber(s); month > 0 && year >= 2000 {
		return fmt.Sprintf("%04d-%02d", year, month)
	}
	return ""
}

func parseDate(s string, date1904 bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "02.01.2006 15:04:05", "2.1.2006 15:04:05", "02.01.2006", "2.1.2006", "2006-01-02", "2006-01-02 15:04:05", "1/2/06", "01/02/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	if v, ok, err := number(s); err == nil && ok && v >= 0 && v < 100000 {
		if t, err := excelize.ExcelDateToTime(v, date1904); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("некорректная дата")
}

func cell(row []string, column int) string {
	if column < 0 || column >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[column])
}

func totalRow(row []string) bool {
	for _, v := range row {
		s := normalize(v)
		if s == "итого" || strings.HasPrefix(s, "итого ") || s == "всего" || s == "общий итог" {
			return true
		}
	}
	return false
}
