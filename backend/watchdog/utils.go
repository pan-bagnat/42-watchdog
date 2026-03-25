package watchdog

import (
	"fmt"
	"time"
)

func formatDuration(d time.Duration) string {
	// Round to nearest second for cleaner output, optional
	d = d.Round(time.Second)

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 && h < 10 {
		return fmt.Sprintf(" (%dh%02dm%02ds) ", h, m, s)
	} else if h >= 10 {
		return fmt.Sprintf("(%02dh%02dm%02ds) ", h, m, s)
	} else if m > 0 {
		return fmt.Sprintf("  (%dm%02ds)   ", m, s)
	} else if s < 10 {
		return fmt.Sprintf("    (%ds)    ", s)
	} else {
		return fmt.Sprintf("    (%02ds)    ", s)
	}
}
