
sshserv: main.go serv/shell_linux.go
	go build -ldflags="-s -w"
	rm -f s.tgz
	tar czf s.tgz sshserv
	#scp s.tgz gpu17:/var/lib/kubelet/taskrun2/

linux:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w"
win:
	GOOS=windows GOARCH=386 go build -ldflags="-s -w"
	cp sshserv.exe d:/devtool/ss/
	cp sshserv.exe dist/

clean:
	rm -f sshserv sshserv.exe cli/cli cli/sftpcli dist/sftpgo.log
