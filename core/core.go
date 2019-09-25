package core

import (
	"./user"
	"./webdav"
	"net/http"
)

func NewUserManger(root string) (*user.Manager, error) {
	manager, err := user.NewManger(root)
	return manager, err
}

func StartWebDav(root, addr string) error {
	userManager, err := user.NewManger(root)
	if err != nil {
		return err
	}

	webDav := webdav.NewServer(root)
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name, password, has := r.BasicAuth()
		if !has {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		user, _ := userManager.Find(name)
		if user == nil || user.Password != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		webDav.ServeWebDav(w, r, webdav.Strategy{SubDir: user.Name, UpRate: user.UpRate, DownRate: user.DownRate})
	})

	server := &http.Server{Addr: addr, Handler: serveMux}
	err = server.ListenAndServe()
	return err
}
