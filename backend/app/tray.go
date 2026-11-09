package app

import (
	"runtime"

	"github.com/egoist/mygo"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/flowlens/resources"
)

type appTray struct {
	tray         *mygo.Tray
	stopLanguage func()
	show, quit   func()
}

// newAppTray adds the tray icon. An error means this desktop has no tray.
func newAppTray(show, quit func()) (*appTray, error) {
	icon, template := resources.AppIcon, false
	if runtime.GOOS == "darwin" {
		icon, template = resources.TrayTemplateIcon, true
	}
	tray := &appTray{show: show, quit: quit}
	native, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "main",
		Icon:           icon,
		IconIsTemplate: template,
		ToolTip:        appservice.APP_NAME,
		Menu:           tray.menu(),
	})
	if err != nil {
		return nil, err
	}
	tray.tray = native
	native.OnClick(show)
	tray.stopLanguage = mygo.App.OnLocaleChanged(func(string) { native.SetMenu(tray.menu()) })
	return tray, nil
}

func (t *appTray) menu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: locales.TrayOpenMainWindow(), Click: func(*mygo.MenuItem, *mygo.Window) { t.show() }},
		{Type: mygo.MenuItemSeparator},
		{Label: locales.TrayClose(), Click: func(*mygo.MenuItem, *mygo.Window) { t.quit() }},
	})
}

func (t *appTray) destroy() {
	t.stopLanguage()
	t.tray.Destroy()
}
