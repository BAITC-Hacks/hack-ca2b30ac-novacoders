package recommendation

import (
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

// HistoryWindow returns the inclusive calendar months used for data validation.
// It does not forecast demand or trim the history sent to the AI service.
func HistoryWindow(asOf time.Time, settings domain.RecommendationSettings) (time.Time, time.Time) {
	last := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, asOf.Location())
	lastDay := last.AddDate(0, 1, -1).Day()
	if settings.ExcludePartialMonth && asOf.Day() < lastDay {
		last = last.AddDate(0, -1, 0)
	}
	return last.AddDate(0, 1-settings.HistoryMonths, 0), last
}
