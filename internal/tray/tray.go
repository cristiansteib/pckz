// Package tray is a minimal StatusNotifierItem + com.canonical.dbusmenu
// implementation (the protocol behind GNOME's AppIndicator extension, KDE and
// most other panels).
//
// It exists instead of a GTK3 AppIndicator so the panel icon and every menu
// item can use *themed* icon names — the shell then recolors the symbolic
// SVGs to match the panel — and so items can combine an icon with a radio
// mark, which GtkMenu-based indicators cannot express.
package tray

import (
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	itemPath    = "/StatusNotifierItem"
	menuPath    = "/MenuBar"
	itemIface   = "org.kde.StatusNotifierItem"
	menuIface   = "com.canonical.dbusmenu"
	watcherName = "org.kde.StatusNotifierWatcher"
)

// Item is one menu entry.
type Item struct {
	Label     string
	Icon      string // themed icon name
	Separator bool
	Header    bool   // a disabled section title
	Toggle    string // "", "radio" or "checkmark"
	Checked   bool
	Disabled  bool
	OnClick   func()

	id int32
}

// visible is everything about an item the panel renders.
func (it *Item) visible() string {
	return fmt.Sprintf("%q|%q|%v|%v|%q|%v|%v", it.Label, it.Icon, it.Separator, it.Header, it.Toggle, it.Checked, it.Disabled)
}

type Tray struct {
	conn  *dbus.Conn
	name  string
	props *prop.Properties

	mu       sync.Mutex
	items    []*Item
	byID     map[int32]*Item
	revision uint32

	OnActivate          func()
	OnSecondaryActivate func()
	OnScroll            func(delta int)
}

// New registers a tray item on the session bus.
func New(id, title, icon, themePath string) (*Tray, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	t := &Tray{conn: conn, byID: map[int32]*Item{}}
	t.name = fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	if _, err := conn.RequestName(t.name, dbus.NameFlagDoNotQueue); err != nil {
		return nil, err
	}

	if err := conn.Export((*sni)(t), itemPath, itemIface); err != nil {
		return nil, err
	}
	if err := conn.Export((*menu)(t), menuPath, menuIface); err != nil {
		return nil, err
	}

	emptyPixmap := []struct {
		W, H int32
		Pix  []byte
	}{}
	t.props, err = prop.Export(conn, itemPath, prop.Map{itemIface: {
		"Category":            {Value: "Hardware", Emit: prop.EmitTrue},
		"Id":                  {Value: id, Emit: prop.EmitTrue},
		"Title":               {Value: title, Emit: prop.EmitTrue},
		"Status":              {Value: "Active", Emit: prop.EmitTrue},
		"WindowId":            {Value: int32(0), Emit: prop.EmitTrue},
		"IconName":            {Value: icon, Emit: prop.EmitTrue},
		"IconThemePath":       {Value: themePath, Emit: prop.EmitTrue},
		"IconPixmap":          {Value: emptyPixmap, Emit: prop.EmitTrue},
		"OverlayIconName":     {Value: "", Emit: prop.EmitTrue},
		"OverlayIconPixmap":   {Value: emptyPixmap, Emit: prop.EmitTrue},
		"AttentionIconName":   {Value: "", Emit: prop.EmitTrue},
		"AttentionIconPixmap": {Value: emptyPixmap, Emit: prop.EmitTrue},
		"AttentionMovieName":  {Value: "", Emit: prop.EmitTrue},
		"ToolTip":             {Value: tooltip{Icon: "", Pixmap: emptyPixmap, Title: title}, Emit: prop.EmitTrue},
		"ItemIsMenu":          {Value: true, Emit: prop.EmitTrue},
		"Menu":                {Value: dbus.ObjectPath(menuPath), Emit: prop.EmitTrue},
	}})
	if err != nil {
		return nil, err
	}
	_, err = prop.Export(conn, menuPath, prop.Map{menuIface: {
		"Version":       {Value: uint32(3), Emit: prop.EmitTrue},
		"TextDirection": {Value: "ltr", Emit: prop.EmitTrue},
		"Status":        {Value: "normal", Emit: prop.EmitTrue},
		"IconThemePath": {Value: []string{themePath}, Emit: prop.EmitTrue},
	}})
	if err != nil {
		return nil, err
	}

	_ = conn.Export(introspect.Introspectable(sniIntrospection), itemPath, "org.freedesktop.DBus.Introspectable")
	_ = conn.Export(introspect.Introspectable(menuIntrospection), menuPath, "org.freedesktop.DBus.Introspectable")

	t.register()
	go t.watchWatcher()
	return t, nil
}

// register announces the item to the panel's StatusNotifierWatcher.
func (t *Tray) register() {
	t.conn.Object(watcherName, "/StatusNotifierWatcher").
		Call(watcherName+".RegisterStatusNotifierItem", 0, t.name)
}

// watchWatcher re-registers when the panel restarts (e.g. GNOME Shell or the
// AppIndicator extension reloading).
func (t *Tray) watchWatcher() {
	_ = t.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherName))
	ch := make(chan *dbus.Signal, 4)
	t.conn.Signal(ch)
	for s := range ch {
		if s.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(s.Body) < 3 {
			continue
		}
		if name, _ := s.Body[0].(string); name != watcherName {
			continue
		}
		if newOwner, _ := s.Body[2].(string); newOwner != "" {
			t.register()
		}
	}
}

type tooltip struct {
	Icon   string
	Pixmap []struct {
		W, H int32
		Pix  []byte
	}
	Title string
	Body  string
}

func (t *Tray) SetIcon(name string) {
	if v, _ := t.props.Get(itemIface, "IconName"); v.Value() == name {
		return
	}
	t.props.SetMust(itemIface, "IconName", name)
	_ = t.conn.Emit(itemPath, itemIface+".NewIcon")
}

func (t *Tray) SetTooltip(title, body string) {
	tt := tooltip{Pixmap: []struct {
		W, H int32
		Pix  []byte
	}{}, Title: title, Body: body}
	if v, _ := t.props.Get(itemIface, "ToolTip"); v.Value() != nil {
		if old, ok := v.Value().(tooltip); ok && old.Title == title && old.Body == body {
			return
		}
	}
	t.props.SetMust(itemIface, "ToolTip", tt)
	_ = t.conn.Emit(itemPath, itemIface+".NewToolTip")
}

// SetMenu replaces the whole menu. The panel is only told to re-read it when
// something visible changed, so frequent refreshes (volume drags) are free.
func (t *Tray) SetMenu(items []*Item) {
	t.mu.Lock()
	same := len(items) == len(t.items)
	for i := 0; same && i < len(items); i++ {
		same = items[i].visible() == t.items[i].visible()
	}
	t.items = items
	t.byID = map[int32]*Item{}
	for i, it := range items {
		it.id = int32(i + 1)
		t.byID[it.id] = it
	}
	if same {
		t.mu.Unlock()
		return
	}
	t.revision++
	rev := t.revision
	t.mu.Unlock()
	_ = t.conn.Emit(menuPath, menuIface+".LayoutUpdated", rev, int32(0))
}

// ── org.kde.StatusNotifierItem ──────────────────────────────────────────────

type sni Tray

func (s *sni) Activate(x, y int32) *dbus.Error {
	if s.OnActivate != nil {
		go s.OnActivate()
	}
	return nil
}

func (s *sni) SecondaryActivate(x, y int32) *dbus.Error {
	if s.OnSecondaryActivate != nil {
		go s.OnSecondaryActivate()
	}
	return nil
}

func (s *sni) ContextMenu(x, y int32) *dbus.Error { return nil }

func (s *sni) Scroll(delta int32, orientation string) *dbus.Error {
	if s.OnScroll != nil && orientation != "horizontal" && delta != 0 {
		go s.OnScroll(int(delta))
	}
	return nil
}

// ── com.canonical.dbusmenu ──────────────────────────────────────────────────

type menu Tray

type layout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

func (m *menu) itemProps(it *Item) map[string]dbus.Variant {
	p := map[string]dbus.Variant{}
	if it.Separator {
		p["type"] = dbus.MakeVariant("separator")
		return p
	}
	p["label"] = dbus.MakeVariant(it.Label)
	if it.Icon != "" {
		p["icon-name"] = dbus.MakeVariant(it.Icon)
	}
	if it.Disabled || it.Header {
		p["enabled"] = dbus.MakeVariant(false)
	}
	if it.Toggle != "" {
		p["toggle-type"] = dbus.MakeVariant(it.Toggle)
		state := int32(0)
		if it.Checked {
			state = 1
		}
		p["toggle-state"] = dbus.MakeVariant(state)
	}
	return p
}

func (m *menu) GetLayout(parentID, depth int32, names []string) (uint32, layout, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if parentID != 0 {
		it, ok := m.byID[parentID]
		if !ok {
			return m.revision, layout{}, dbus.MakeFailedError(fmt.Errorf("no item %d", parentID))
		}
		return m.revision, layout{ID: parentID, Props: m.itemProps(it), Children: []dbus.Variant{}}, nil
	}
	root := layout{ID: 0, Props: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}, Children: []dbus.Variant{}}
	if depth != 0 {
		for _, it := range m.items {
			root.Children = append(root.Children, dbus.MakeVariant(layout{ID: it.id, Props: m.itemProps(it), Children: []dbus.Variant{}}))
		}
	}
	return m.revision, root, nil
}

type idProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

func (m *menu) GetGroupProperties(ids []int32, names []string) ([]idProps, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []idProps
	for _, id := range ids {
		if it, ok := m.byID[id]; ok {
			out = append(out, idProps{id, m.itemProps(it)})
		}
	}
	return out, nil
}

func (m *menu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if it, ok := m.byID[id]; ok {
		if v, ok := m.itemProps(it)[name]; ok {
			return v, nil
		}
	}
	return dbus.MakeVariant(""), nil
}

func (m *menu) Event(id int32, eventID string, data dbus.Variant, ts uint32) *dbus.Error {
	if eventID != "clicked" {
		return nil
	}
	m.mu.Lock()
	it := m.byID[id]
	m.mu.Unlock()
	if it != nil && it.OnClick != nil && !it.Disabled && !it.Header {
		go it.OnClick()
	}
	return nil
}

type event struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

func (m *menu) EventGroup(events []event) ([]int32, *dbus.Error) {
	for _, e := range events {
		m.Event(e.ID, e.EventID, e.Data, e.Timestamp)
	}
	return []int32{}, nil
}

func (m *menu) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

func (m *menu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}

const sniIntrospection = `<node>
 <interface name="org.kde.StatusNotifierItem">
  <property name="Category" type="s" access="read"/>
  <property name="Id" type="s" access="read"/>
  <property name="Title" type="s" access="read"/>
  <property name="Status" type="s" access="read"/>
  <property name="WindowId" type="i" access="read"/>
  <property name="IconName" type="s" access="read"/>
  <property name="IconThemePath" type="s" access="read"/>
  <property name="IconPixmap" type="a(iiay)" access="read"/>
  <property name="OverlayIconName" type="s" access="read"/>
  <property name="OverlayIconPixmap" type="a(iiay)" access="read"/>
  <property name="AttentionIconName" type="s" access="read"/>
  <property name="AttentionIconPixmap" type="a(iiay)" access="read"/>
  <property name="AttentionMovieName" type="s" access="read"/>
  <property name="ToolTip" type="(sa(iiay)ss)" access="read"/>
  <property name="ItemIsMenu" type="b" access="read"/>
  <property name="Menu" type="o" access="read"/>
  <method name="ContextMenu"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
  <method name="Activate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
  <method name="SecondaryActivate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
  <method name="Scroll"><arg name="delta" type="i" direction="in"/><arg name="orientation" type="s" direction="in"/></method>
  <signal name="NewTitle"/>
  <signal name="NewIcon"/>
  <signal name="NewAttentionIcon"/>
  <signal name="NewOverlayIcon"/>
  <signal name="NewToolTip"/>
  <signal name="NewStatus"><arg name="status" type="s"/></signal>
 </interface>
` + introspect.IntrospectDataString + `
 <interface name="org.freedesktop.DBus.Properties">
  <method name="Get"><arg name="interface" type="s" direction="in"/><arg name="property" type="s" direction="in"/><arg name="value" type="v" direction="out"/></method>
  <method name="GetAll"><arg name="interface" type="s" direction="in"/><arg name="props" type="a{sv}" direction="out"/></method>
  <method name="Set"><arg name="interface" type="s" direction="in"/><arg name="property" type="s" direction="in"/><arg name="value" type="v" direction="in"/></method>
  <signal name="PropertiesChanged"><arg name="interface" type="s"/><arg name="changed_properties" type="a{sv}"/><arg name="invalidated_properties" type="as"/></signal>
 </interface>
</node>`

const menuIntrospection = `<node>
 <interface name="com.canonical.dbusmenu">
  <property name="Version" type="u" access="read"/>
  <property name="TextDirection" type="s" access="read"/>
  <property name="Status" type="s" access="read"/>
  <property name="IconThemePath" type="as" access="read"/>
  <method name="GetLayout">
   <arg type="i" name="parentId" direction="in"/><arg type="i" name="recursionDepth" direction="in"/><arg type="as" name="propertyNames" direction="in"/>
   <arg type="u" name="revision" direction="out"/><arg type="(ia{sv}av)" name="layout" direction="out"/>
  </method>
  <method name="GetGroupProperties">
   <arg type="ai" name="ids" direction="in"/><arg type="as" name="propertyNames" direction="in"/><arg type="a(ia{sv})" name="properties" direction="out"/>
  </method>
  <method name="GetProperty">
   <arg type="i" name="id" direction="in"/><arg type="s" name="name" direction="in"/><arg type="v" name="value" direction="out"/>
  </method>
  <method name="Event">
   <arg type="i" name="id" direction="in"/><arg type="s" name="eventId" direction="in"/><arg type="v" name="data" direction="in"/><arg type="u" name="timestamp" direction="in"/>
  </method>
  <method name="EventGroup">
   <arg type="a(isvu)" name="events" direction="in"/><arg type="ai" name="idErrors" direction="out"/>
  </method>
  <method name="AboutToShow">
   <arg type="i" name="id" direction="in"/><arg type="b" name="needUpdate" direction="out"/>
  </method>
  <method name="AboutToShowGroup">
   <arg type="ai" name="ids" direction="in"/><arg type="ai" name="updatesNeeded" direction="out"/><arg type="ai" name="idErrors" direction="out"/>
  </method>
  <signal name="ItemsPropertiesUpdated"><arg type="a(ia{sv})" name="updatedProps"/><arg type="a(ias)" name="removedProps"/></signal>
  <signal name="LayoutUpdated"><arg type="u" name="revision"/><arg type="i" name="parent"/></signal>
  <signal name="ItemActivationRequested"><arg type="i" name="id"/><arg type="u" name="timestamp"/></signal>
 </interface>
` + introspect.IntrospectDataString + `
</node>`
