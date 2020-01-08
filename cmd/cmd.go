package cmd

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"runtime"
	"simple-webdav/core"
	"simple-webdav/core/user"
	"strconv"
	"time"
)

func Execute() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	h := flag.Bool("h", false, "this help!")
	d := flag.Bool("d", false, "is daemon?")

	a := flag.String("a", ":8888", "listening `address` for server!")
	r := flag.String("r", "./storage", "root `path` for server!")
	s := flag.String("s", "", "`start|stop` for server!")
	u := flag.String("u", "", "`add|update|delete|find|list` user operations!")
	flag.Parse()

	if *h {
		flag.Usage()
	}

	if *r != "" {
		os.MkdirAll(path.Join(*r), os.ModePerm)
	}

	if *s != "" {
		manageServer(*r, *s, *a, *d)
	}

	if *u != "" {
		manageUser(*r, *u, flag.Args()...)
	}
}

func manageUser(root, operate string, args ...string) {
	nArg := len(args)
	userManager, err := core.NewUserManger(root)
	if err != nil {
		fmt.Println("Init user manager error!", err)
		return
	}

	switch operate {
	case "add", "update":
		now := time.Now()
		user := user.User{CreateTime: now.Unix(), UpdateTime: now.Unix(), IsValid: 1}
		if nArg < 2 {
			fmt.Println("Parameter error!", args)
			return
		}
		if nArg >= 2 {
			user.Name = args[0]
			user.Password = args[1]
		}
		if nArg >= 3 {
			user.UpRate, _ = strconv.ParseInt(args[2], 10, 64)
		}
		if nArg >= 4 {
			user.DownRate, _ = strconv.ParseInt(args[3], 10, 64)
		}
		if operate == "add" {
			ok, err := userManager.Insert(user)
			if ok {
				os.MkdirAll(path.Join(root, user.Name), os.ModePerm)
				fmt.Println("Successfully added user!")
			} else {
				fmt.Println("Failure to add user!", err)
			}
		} else {
			ok, err := userManager.Update(user.Name, &user)
			if ok {
				fmt.Println("Successfully update user!")
			} else {
				fmt.Println("Failure to update user!", err)
			}
		}
		break

	case "delete":
		name := args[0]
		ok, err := userManager.Delete(name)
		if ok {
			os.RemoveAll(path.Join(root, name))
			fmt.Println("Successfully deleted user!")
		} else {
			fmt.Println("Failure to delete user!", err)
		}
		break

	case "find":
		record, err := userManager.Find(args[0])
		if record != nil {
			userManager.Print([]*user.User{record})
		} else {
			fmt.Println("Failure to find user!", err)
		}
		break

	case "list":
		start := 0
		size := 10
		if nArg >= 1 {
			start, _ = strconv.Atoi(args[0])
		}
		if nArg >= 2 {
			size, _ = strconv.Atoi(args[1])
		}
		total, users, err := userManager.Query("", start, size)
		if err == nil {
			userManager.Print(users)
			fmt.Printf("Total:%d, start:%d, size:%d\n", total, start, size)
		} else {
			fmt.Println("Failure to query user!", err)
		}
		break

	default:
		fmt.Println("Unknown operation!")
	}
}

func manageServer(root, operate, addr string, daemon bool) {
	pidFile := path.Join(root, "./pid.lock")

	switch operate {
	case "start":
		fmt.Println("Starting!")
		if daemon {
			cmd := exec.Command(os.Args[0], "-a", addr, "-r", root, "-s", "start")
			err := cmd.Start()
			if err == nil {
				fmt.Printf("PID %d is running...\n", cmd.Process.Pid)
			} else {
				fmt.Println("Start failed!", err.Error())
			}
			os.Exit(0)
		} else {
			pid := fmt.Sprintf("%d", os.Getpid())
			err := ioutil.WriteFile(pidFile, []byte(pid), 0666)
			if err != nil {
				fmt.Println("Start failed!", err.Error())
			}
			err = core.StartWebDav(root, addr)
			if err != nil {
				fmt.Println("Start failed!", err.Error())
				os.Remove(pidFile)
			}
		}
		break

	case "stop":
		fmt.Println("Stopping!")
		pb, err := ioutil.ReadFile(pidFile)
		if err != nil {
			fmt.Println("Read PID error!", err)
			return
		}

		pid := string(pb)
		cmd := new(exec.Cmd)
		if runtime.GOOS == "windows" {
			cmd = exec.Command("taskkill", "/f", "/pid", pid)
		} else {
			cmd = exec.Command("kill", pid)
		}
		err = cmd.Start()
		if err == nil {
			fmt.Printf("PID %s has been stopped!\n", pid)
			os.Remove(pidFile)
		} else {
			fmt.Println("PID "+pid+" stop failed! %s\n", err)
		}
		break

	default:
		fmt.Println("Unknown operation!")
	}
}
