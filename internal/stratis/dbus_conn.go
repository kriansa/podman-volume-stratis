package stratis

import (
	"github.com/godbus/dbus/v5"
)

// DBusConnection abstracts the godbus connection for testability
type DBusConnection interface {
	// Object returns a BusObject for the given destination and path
	Object(dest string, path dbus.ObjectPath) dbus.BusObject
	// Close closes the connection
	Close() error
}

// systemDBusConnection wraps *dbus.Conn to implement DBusConnection
type systemDBusConnection struct {
	conn *dbus.Conn
}

func (c *systemDBusConnection) Object(dest string, path dbus.ObjectPath) dbus.BusObject {
	return c.conn.Object(dest, path)
}

func (c *systemDBusConnection) Close() error {
	return c.conn.Close()
}

// ConnectSystemBus connects to the system DBus and returns a DBusConnection
func ConnectSystemBus() (DBusConnection, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &systemDBusConnection{conn: conn}, nil
}
