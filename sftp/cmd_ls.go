// Copyright (c) 2022 Blacknon. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// NOTE:
// The file in which code for the sort function used mainly with the lsftp ls command is written.

package sftp

// TODO(blacknon): 複数ホスト接続時に、diffオプションがあるとうれしい？(ファイルの存在有無などをdiffで確認できるといい感じだろうか？)

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/blacknon/lssh/common"
	"github.com/blacknon/textcol"
	"github.com/dustin/go-humanize"
	"github.com/pkg/sftp"
	"github.com/urfave/cli"
)

// sftpLs
type sftpLs struct {
	Client *TargetConnectMap
	Files  []sftpFileInfo
	Passwd string
	Groups string
}

// ls exec and print out remote ls data.
func (r *RunSftp) ls(args []string) (err error) {
	// create app
	app := cli.NewApp()
	// app.UseShortOptionHandling = true

	// set help message
	app.CustomAppHelpTemplate = helptext

	// set parameter
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "1", Usage: "list one file per line"},
		cli.BoolFlag{Name: "a", Usage: "do not ignore entries starting with"},
		cli.BoolFlag{Name: "f", Usage: "do not sort"},
		cli.BoolFlag{Name: "h", Usage: "with -l, print sizes like 1K 234M 2G etc."},
		cli.BoolFlag{Name: "l", Usage: "use a long listing format"},
		cli.BoolFlag{Name: "n", Usage: "list numeric user and group IDs"},
		cli.BoolFlag{Name: "r", Usage: "reverse order while sorting"},
		cli.BoolFlag{Name: "S", Usage: "sort by file size, largest first"},
		cli.BoolFlag{Name: "t", Usage: "sort by modification time, newest first"},
	}
	app.Name = "ls"
	app.Usage = "lsftp build-in command: ls [remote machine ls]"
	app.ArgsUsage = "[host,host...:][PATH]..."
	app.HideHelp = true
	app.HideVersion = true
	app.EnableBashCompletion = true

	// action
	app.Action = func(c *cli.Context) error {
		// argpath
		argData := c.Args()

		targetmap := map[string]*TargetConnectMap{}

		if len(argData) > 0 {
			for _, arg := range argData {
				// sftp target host
				targetmap = r.createTargetMap(targetmap, arg)
			}
		} else {
			for server, client := range r.Client {
				// sftp target host
				targetmap[server] = &TargetConnectMap{}
				targetmap[server].SftpConnect = *client
			}
		}

		r.executeRemoteLs(c, targetmap)

		return nil
	}

	// parse short options
	args = common.ParseArgs(app.Flags, args)
	app.Run(args)

	return
}

func (r *RunSftp) executeRemoteLs(c *cli.Context, clients map[string]*TargetConnectMap) {
	lsdata := map[string]sftpLs{}
	exit := make(chan bool)
	m := new(sync.Mutex)
	for s, cl := range clients {
		server := s
		client := cl

		// Get required data at ls, is obtained in parallel from each server.
		go func() {
			// get output
			client.Output.Create(server)
			w := client.Output.NewWriter()

			// set target directory
			if len(client.Path) > 0 {
				for i, path := range client.Path {
					// TODO: チルダの処理をするならもっとあとのほうがいいかも？配置箇所が違うかも？
					client.Path[i] = path
				}
			} else {
				client.Path = append(client.Path, client.Pwd)
			}

			// get ls data
			data, err := r.getRemoteLsData(client)
			if err != nil {
				fmt.Fprintf(w, "Error: %s\n", err)
				exit <- true
				return
			}

			if !c.Bool("a") {
			}

			// hidden delete data slice
			hddata := []sftpFileInfo{}

			// regex
			rgx := regexp.MustCompile(`^\.`)

			for _, f := range data.Files {
				// if `a` flag disable, delete Hidden files...
				if !c.Bool("a") {
					if !rgx.MatchString(f.Name()) {
						hddata = append(hddata, f)
					}
					continue
				}

				hddata = append(hddata, f)

			}

			// sort
			r.SortLsData(c, hddata)

			data.Files = hddata

			// write lsdata
			m.Lock()
			lsdata[server] = data
			m.Unlock()

			exit <- true
		}()
	}

	// wait get directory data
	for i := 0; i < len(clients); i++ {
		<-exit
	}

	switch {
	case c.Bool("l"): // long list format
		// set tabwriter
		tabw := new(tabwriter.Writer)
		tabw.Init(os.Stdout, 0, 1, 1, ' ', 0)

		// get maxSizeWidth
		var maxSizeWidth int
		var sizestr string
		for _, data := range lsdata {
			for _, f := range data.Files {
				if c.Bool("h") {
					sizestr = humanize.Bytes(uint64(f.Size()))
				} else {
					sizestr = strconv.FormatUint(uint64(f.Size()), 10)
				}

				// set sizestr max length
				if maxSizeWidth < len(sizestr) {
					maxSizeWidth = len(sizestr)
				}
			}
		}

		// print list ls
		for server, data := range lsdata {
			// get prompt
			data.Client.Output.Create(server)
			prompt := data.Client.Output.GetPrompt()

			// for get data
			datas := []*sftpLsData{}
			for _, f := range lsdata[server].Files {
				sys := f.Sys()

				// TODO(blacknon): count hardlink (2列目)の取得方法がわからないため、わかったら追加。
				var uid, gid uint32
				var size uint64
				var user, group, timestr, sizestr string

				if stat, ok := sys.(*sftp.FileStat); ok {
					uid = stat.UID
					gid = stat.GID
					size = stat.Size
					timestamp := time.Unix(int64(stat.Mtime), 0)
					timestr = timestamp.Format("2006 01-02 15:04:05")
				}

				// Switch with or without -n option.
				if c.Bool("n") {
					user = strconv.FormatUint(uint64(uid), 10)
					group = strconv.FormatUint(uint64(gid), 10)
				} else {
					user, _ = common.GetNameFromId(lsdata[server].Passwd, uid)
					group, _ = common.GetNameFromId(lsdata[server].Groups, gid)
				}

				// Switch with or without -h option.
				if c.Bool("h") {
					sizestr = humanize.Bytes(size)
				} else {
					sizestr = strconv.FormatUint(size, 10)
				}

				// set data
				data := new(sftpLsData)
				data.Mode = f.Mode().String()
				data.User = user
				data.Group = group
				data.Size = sizestr
				data.Time = timestr
				data.Path = filepath.Join(f.Dir, f.Name())

				// append data
				datas = append(datas, data)

				if len(lsdata) == 1 {
					// set print format
					format := "%s\t%s\t%s\t%" + strconv.Itoa(maxSizeWidth) + "s\t%s\t%s\n"

					// write data
					fmt.Fprintf(tabw, format, data.Mode, data.User, data.Group, data.Size, data.Time, data.Path)
				} else {
					// set print format
					format := "%s\t%s\t%s\t%s\t%" + strconv.Itoa(maxSizeWidth) + "s\t%s\t%s\n"

					// write data
					fmt.Fprintf(tabw, format, prompt, data.Mode, data.User, data.Group, data.Size, data.Time, data.Path)
				}
			}
		}

		tabw.Flush()

	case c.Bool("1"): // list 1 file per line
		// for list
		for server, data := range lsdata {
			data.Client.Output.Create(server)
			w := data.Client.Output.NewWriter()

			for _, f := range data.Files {
				name := filepath.Join(f.Dir, f.Name())

				fmt.Fprintf(w, "%s\n", name)
			}
		}

	default: // default
		for server, data := range lsdata {
			// get header width
			data.Client.Output.Create(server)
			w := data.Client.Output.NewWriter()
			headerWidth := len(data.Client.Output.Prompt)

			var item []string
			for _, f := range data.Files {
				item = append(item, f.Name())
			}

			textcol.Output = w
			textcol.Padding = headerWidth
			textcol.PrintColumns(&item, 2)
		}
	}
}

// getRemoteLsData
func (r *RunSftp) getRemoteLsData(client *TargetConnectMap) (lsdata sftpLs, err error) {
	w := client.Output.NewWriter()

	data := []sftpFileInfo{}
	re := regexp.MustCompile(`(.+)/$`)

	for _, ep := range client.Path {
		ep = re.ReplaceAllString(ep, "$1")

		// get glob
		epath, err := ExpandRemotePath(client, ep)
		if err != nil {
			fmt.Fprintf(w, "Error: %s\n", err)
			continue
		}

		if len(epath) == 0 {
			fmt.Fprintf(w, "Error: %s not found.\n", ep)
			continue
		}

		for _, path := range epath {
			// get symlink
			p, err := client.Connect.ReadLink(path)
			if err == nil {
				path = p
			}

			// get stat
			lstat, err := client.Connect.Lstat(path)
			if err != nil {
				fmt.Fprintf(w, "Error: %s\n", err)
				continue
			}

			// get path data
			if lstat.IsDir() {
				// get directory list data
				lsdata, err := client.Connect.ReadDir(path)
				if err != nil {
					fmt.Fprintf(w, "Error: %s\n", err)
					continue
				}

				if len(lsdata) == 0 {
					dir := filepath.Dir(path)
					fi := sftpFileInfo{
						FileInfo: lstat,
						Dir:      dir,
					}
					data = append(data, fi)
				}

				for _, d := range lsdata {
					dir := path
					fi := sftpFileInfo{
						FileInfo: d,
						Dir:      dir,
					}

					data = append(data, fi)
				}

			} else {
				dir := filepath.Dir(path)
				fi := sftpFileInfo{
					FileInfo: lstat,
					Dir:      dir,
				}

				data = append(data, fi)
			}
		}
	}

	// read /etc/passwd
	passwdFile, err := client.Connect.Open("/etc/passwd")
	if err != nil {
		return
	}
	passwdByte, err := ioutil.ReadAll(passwdFile)
	if err != nil {
		return
	}
	passwd := string(passwdByte)

	// read /etc/group
	groupFile, err := client.Connect.Open("/etc/group")
	if err != nil {
		return
	}
	groupByte, err := ioutil.ReadAll(groupFile)
	if err != nil {
		return
	}
	groups := string(groupByte)

	// set lsdata
	lsdata = sftpLs{
		Client: client,
		Files:  data,
		Passwd: passwd,
		Groups: groups,
	}

	return
}
