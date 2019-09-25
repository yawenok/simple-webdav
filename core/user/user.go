package user

import (
	"fmt"
	"github.com/go-xorm/xorm"
	_ "github.com/mattn/go-sqlite3"
	"path"
)

type User struct {
	Id         int64  `xorm:"pk autoincr"`
	Name       string `xorm:"VARCHAR(255) unique"`
	Password   string `xorm:"VARCHAR(255)"`
	UpRate     int64  `xorm:"default 0"`
	DownRate   int64  `xorm:"default 0"`
	MaxVolume  int64  `xorm:"default 0"`
	CreateTime int64  `xorm:"created"`
	UpdateTime int64  `xorm:"updated"`
	IsValid    int    `xorm:"default 1"`
}

type Manager struct {
	x *xorm.Engine
}

func NewManger(root string) (*Manager, error) {
	x, err := xorm.NewEngine("sqlite3", path.Join(root, "./user.db"))
	if err != nil {
		return nil, err
	}
	if err := x.Sync(new(User)); err != nil {
		return nil, err
	}

	return &Manager{x: x}, nil
}

func (m *Manager) Query(name string, start, size int) (int64, []*User, error) {
	sql := "1=1"
	if name != "" {
		sql = "name like ?"
	}

	total, err := m.x.Where(sql, name).Count(new(User))
	if err != nil {
		return 0, nil, err
	}

	users := make([]*User, 0)
	err = m.x.Where(sql, name).Limit(size, start).Find(&users)
	if err != nil {
		return 0, nil, err
	}

	return total, users, nil
}

func (m *Manager) Insert(user User) (bool, error) {
	affected, err := m.x.Insert(user)
	if err != nil {
		return false, err
	}

	return affected == 1, err
}

func (m *Manager) Delete(name string) (bool, error) {
	userRecord, err := m.Find(name)
	if userRecord == nil {
		return false, err
	}

	affected, err := m.x.Id(userRecord.Id).Delete(userRecord)
	if err != nil {
		return false, err
	}

	return affected == 1, nil
}

func (m *Manager) Update(name string, user *User) (bool, error) {
	userRecord, err := m.Find(name)
	if userRecord == nil {
		return false, err
	}

	affected, err := m.x.ID(userRecord.Id).Update(user)
	if err != nil {
		return false, err
	}

	return affected == 1, nil
}

func (m *Manager) Find(name string) (*User, error) {
	user := new(User)

	has, err := m.x.Where("name=?", name).Get(user)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	return user, err
}

func (m *Manager) Print(users []*User) {
	fmt.Printf("%-20s %-20s %-20s %-20s %-20s %-20s\n", "Name", "Password", "UpRate", "DownRate", "CreateTime", "UpdateTime")
	for i := 0; i < len(users); i++ {
		user := users[i]
		fmt.Printf("%-20s %-20s %-20d %-20d %-20d %-20d\n", user.Name, user.Password, user.UpRate, user.DownRate, user.CreateTime, user.UpdateTime)
	}
}
