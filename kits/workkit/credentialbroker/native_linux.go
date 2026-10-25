//go:build linux

package credentialbroker

import (
	"context"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/godbus/dbus/v5"
	"os"
	"strconv"
	"time"
)

type secretService struct{ address, namespace string }
type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func openNative(c a.BrokerConfig) (Store, error) {
	expected := "unix:path=/run/user/" + strconv.Itoa(os.Geteuid()) + "/bus"
	if c.BusAddress != expected {
		return nil, errors.New("explicit current-user bus required")
	}
	return secretService{expected, c.Namespace}, nil
}
func (s secretService) session(ctx context.Context) (*dbus.Conn, dbus.ObjectPath, error) {
	conn, e := dbus.Dial(s.address, dbus.WithContext(ctx))
	if e != nil {
		return nil, "", e
	}
	if e = conn.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Geteuid()))}); e == nil {
		e = conn.Hello()
	}
	if e != nil {
		conn.Close()
		return nil, "", e
	}
	var output dbus.Variant
	var session dbus.ObjectPath
	e = conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").CallWithContext(ctx, "org.freedesktop.Secret.Service.OpenSession", dbus.FlagNoAutoStart, "plain", dbus.MakeVariant("")).Store(&output, &session)
	if e != nil {
		conn.Close()
		return nil, "", e
	}
	return conn, session, nil
}
func (s secretService) items(ctx context.Context, c *dbus.Conn, key string) ([]dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	e := c.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").CallWithContext(ctx, "org.freedesktop.Secret.Service.SearchItems", dbus.FlagNoAutoStart, map[string]string{"application": "workkit-" + s.namespace, "scope": key}).Store(&unlocked, &locked)
	if e != nil || len(locked) > 0 || len(unlocked) > 1 {
		return nil, errors.New("locked or ambiguous native item")
	}
	return unlocked, nil
}
func (s secretService) Read(ctx context.Context, key string) ([]byte, error) {
	c, session, e := s.session(ctx)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	defer closeSession(c, session)
	items, e := s.items(ctx, c, key)
	if e != nil {
		return nil, e
	}
	if len(items) == 0 {
		return nil, ErrAbsent
	}
	var v secret
	e = c.Object("org.freedesktop.secrets", items[0]).CallWithContext(ctx, "org.freedesktop.Secret.Item.GetSecret", dbus.FlagNoAutoStart, session).Store(&v)
	if e != nil || len(v.Value) > 16384 || v.Session != session {
		return nil, errors.New("native secret unavailable")
	}
	return v.Value, nil
}
func (s secretService) Write(ctx context.Context, key string, b []byte) error {
	c, session, e := s.session(ctx)
	if e != nil {
		return e
	}
	defer c.Close()
	defer closeSession(c, session)
	items, e := s.items(ctx, c, key)
	if e != nil {
		return e
	}
	v := secret{session, []byte{}, b, "application/json"}
	if len(items) == 1 {
		return c.Object("org.freedesktop.secrets", items[0]).CallWithContext(ctx, "org.freedesktop.Secret.Item.SetSecret", dbus.FlagNoAutoStart, v).Err
	}
	var collection dbus.ObjectPath
	e = c.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").CallWithContext(ctx, "org.freedesktop.Secret.Service.ReadAlias", dbus.FlagNoAutoStart, "default").Store(&collection)
	if e != nil || collection == "/" {
		return errors.New("default collection unavailable")
	}
	props := map[string]dbus.Variant{"org.freedesktop.Secret.Item.Label": dbus.MakeVariant("WorkKit"), "org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(map[string]string{"application": "workkit-" + s.namespace, "scope": key})}
	var item, prompt dbus.ObjectPath
	e = c.Object("org.freedesktop.secrets", collection).CallWithContext(ctx, "org.freedesktop.Secret.Collection.CreateItem", dbus.FlagNoAutoStart, props, v, false).Store(&item, &prompt)
	if e != nil || prompt != "/" {
		return errors.New("native creation needs external unlock")
	}
	return nil
}

func closeSession(c *dbus.Conn, session dbus.ObjectPath) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.Object("org.freedesktop.secrets", session).CallWithContext(ctx, "org.freedesktop.Secret.Session.Close", dbus.FlagNoAutoStart)
}
