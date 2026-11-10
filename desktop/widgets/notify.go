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
	timeout  time.Duration
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
	n.addTimed(severity, detail, title, 0)
}

func (n *Notifier) addTimed(severity Severity, detail, title string, timeout time.Duration) {
	n.pending = append(n.pending, notification{severity: severity, title: title, detail: detail, timeout: timeout})
}

// WithTimeout returns a notifier whose notifications stay for d instead of
// their severity's default, for operations whose result needs reading time.
func (n *Notifier) WithTimeout(d time.Duration) TimedNotifier {
	return TimedNotifier{notifier: n, timeout: d}
}

// TimedNotifier adds notifications that stay for a set time.
type TimedNotifier struct {
	notifier *Notifier
	timeout  time.Duration
}

// Success reports a finished operation.
func (n TimedNotifier) Success(detail string) {
	n.notifier.addTimed(SeveritySuccess, detail, "", n.timeout)
}

// Info reports something the user should know.
func (n TimedNotifier) Info(detail string) { n.notifier.addTimed(SeverityInfo, detail, "", n.timeout) }

// Warning reports a problem that does not stop the user.
func (n TimedNotifier) Warning(detail string) {
	n.notifier.addTimed(SeverityWarning, detail, "", n.timeout)
}

// Error reports a failed operation.
func (n TimedNotifier) Error(detail string) {
	n.notifier.addTimed(SeverityError, detail, "", n.timeout)
}

// Titled adds a notification with a heading above its detail.
func (n TimedNotifier) Titled(severity Severity, title, detail string) {
	n.notifier.addTimed(severity, detail, title, n.timeout)
}

// Pending returns how many notifications wait to be shown.
func (n *Notifier) Pending() int { return len(n.pending) }

// ToastViewport places a window's notifications top-center, as FlowLens
// 1.0.5 did in every window. Build it once per frame, after Notifier.Show.
func ToastViewport(c *ui.Context, theme nui.Theme) ui.Element {
	return nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 3, Position: nui.ToastTopCenter})
}

// Show displays the queued notifications. Build it once per frame, before the
// window's ToastViewport.
func (n *Notifier) Show(c *ui.Context, theme nui.Theme) {
	pending := n.pending
	n.pending = nil
	for _, item := range pending {
		timeout := item.timeout
		if timeout <= 0 {
			timeout = severityTimeout(item.severity)
		}
		options := nui.ToastOptions{
			Title:    item.detail,
			Color:    severityColor(item.severity),
			Icon:     severityIcon(item.severity),
			Timeout:  timeout,
			Progress: nui.Set(false),
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
