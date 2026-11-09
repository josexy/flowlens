// Package widgets holds the controls FlowLens builds on the component library.
package widgets

import (
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// Severity is how important a notification is.
type Severity int

// Severities.
const (
	SeveritySuccess Severity = iota
	SeverityInfo
	SeverityWarning
	SeverityError
)

// How long a notification stays by default.
const (
	ordinaryNotificationTime = 1800 * time.Millisecond
	warningNotificationTime  = 3 * time.Second
	errorNotificationTime    = 4 * time.Second
)

type notification struct {
	severity Severity
	title    string
	detail   string
}

// Notifier queues notifications for one window. Stores and callbacks add to it
// on the main thread, and the window's view shows them.
type Notifier struct {
	pending []notification
}

// Success reports a finished operation.
func (n *Notifier) Success(detail string) { n.add(SeveritySuccess, detail, "") }

// Info reports something the user should know.
func (n *Notifier) Info(detail string) { n.add(SeverityInfo, detail, "") }

// Warning reports a problem that does not stop the user.
func (n *Notifier) Warning(detail string) { n.add(SeverityWarning, detail, "") }

// Error reports a failed operation.
func (n *Notifier) Error(detail string) { n.add(SeverityError, detail, "") }

// Titled adds a notification with a heading above its detail.
func (n *Notifier) Titled(severity Severity, title, detail string) { n.add(severity, detail, title) }

func (n *Notifier) add(severity Severity, detail, title string) {
	n.pending = append(n.pending, notification{severity: severity, title: title, detail: detail})
}

// Pending returns how many notifications wait to be shown.
func (n *Notifier) Pending() int { return len(n.pending) }

// Show displays the queued notifications. Build it once per frame, before the
// window's ToastViewport.
func (n *Notifier) Show(c *ui.Context, theme nui.Theme) {
	pending := n.pending
	n.pending = nil
	for _, item := range pending {
		options := nui.ToastOptions{
			Title:   item.detail,
			Color:   severityColor(item.severity),
			Icon:    severityIcon(item.severity),
			Timeout: severityTimeout(item.severity),
		}
		if item.title != "" {
			options.Title, options.Description = item.title, item.detail
		}
		nui.Toast(c, theme, options)
	}
}

func severityColor(severity Severity) nui.Color {
	switch severity {
	case SeverityInfo:
		return nui.Info
	case SeverityWarning:
		return nui.Warning
	case SeverityError:
		return nui.Error
	default:
		return nui.Success
	}
}

func severityIcon(severity Severity) *ui.SVG {
	switch severity {
	case SeverityInfo:
		return lucide.Info()
	case SeverityWarning:
		return lucide.TriangleAlert()
	case SeverityError:
		return lucide.CircleX()
	default:
		return lucide.CircleCheck()
	}
}

func severityTimeout(severity Severity) time.Duration {
	switch severity {
	case SeverityWarning:
		return warningNotificationTime
	case SeverityError:
		return errorNotificationTime
	default:
		return ordinaryNotificationTime
	}
}
