## webhdfs 
webhdfs is a Go bindings for Hadoop HDFS via its WebHDFS interface.  

forked from https://github.com/extrame/webhdfs, mostly to move a defered close() in the lib that causes a memory leak

It provides typed access to remote HDFS resources via Go's JSON marshaling system.  It follows the WebHDFS JSON protocol outline in  http://hadoop.apache.org/docs/current/hadoop-project-dist/hadoop-hdfs/WebHDFS.html.  It has been tested with Apache Hadoop 2.x.x - series.

### Usage
```
go get github.com/orangeforjava/gowebhdfs
```
```go
import "github.com/orangeforjava/gowebhdfs"
...
fs, err := webhdfs.NewFileSystem(webhdfs.Configuration{Addr: "localhost:50070", User: "hdfs"})
if err != nil{
	log.Fatal(err)
}
checksum, err := fs.GetFileChecksum(webhdfs.Path{Name: "location/to/file"})
if err != nil {
	log.Fatal(err)
}
fmt.Println (checksum)
```

#### HDFS Setup
* Enable `dfs.webhdfs.enabled` property in your hsdfs-site.xml 
* Ensure `hadoop.http.staticuser.user` property is set in your core-site.xml.


## API Overview
webhdfs lets you access HDFS resources via two structs `FileSystem` and `FsShell`.  Use FileSystem to get access to low level callse.  FsShell is designed to provide a higer level of abstraction and integration with the local file system.

### FileSystem API 
#### Configuration{} Struct
Use the `Configuration{}` struct to specify paramters for the file system.  You can create configuration either using a `Configuration{}` literal or using `NewConfiguration()` for defaults. 

```
conf := *webhdfs.NewConfiguration()
conf.Addr = "localhost:50070"
conf.User = "hdfs"
conf.ConnectionTime = time.Second * 15
conf.DisableKeepAlives = false 
conf.ChecksumWorkers = 4
```

#### FileSystem{} Struct
Create a new `FileSystem{}` struct before you can make call to any functions.  You create the FileSystem by passing in a `Configuration` pointer as shown below. 
```
fs, err := webhdfs.NewFileSystem(conf)
```
Now you are ready to communicate with HDFS.

#### Create File
`FileSystem.Create()` creates and store a remote file on the HDFS server. By default it verifies a local file reader against HDFS `GETFILECHECKSUM` after upload. Set `DisableChecksumVerification` to `true` for stream-only writers that cannot be re-read locally.
```
file, err := os.Open("local-file")
ok, err := fs.Create(
    file,
	webhdfs.Path{Name:"/remote/file"},
	false,
	0,
	0,
	0700,
	0,
)
```

#### Checksum Verification
Local files can be compared with HDFS files without downloading the HDFS file:
```go
result, err := fs.CompareLocalFileChecksum("local-file", webhdfs.Path{Name:"/remote/file"})
if err != nil {
    log.Fatal(err)
}
fmt.Println(result.Match, result.LocalChecksum, result.RemoteChecksum)
```

If you already have the remote metadata, compute the local checksum directly:
```go
status, _ := fs.GetFileStatus(webhdfs.Path{Name:"/remote/file"})
remote, _ := fs.GetFileChecksum(webhdfs.Path{Name:"/remote/file"})
spec, _ := webhdfs.ChecksumSpecFromRemote(status, remote, 4)
local, _ := webhdfs.ComputeLocalFileChecksum("local-file", spec)
```

`FsShell.Get()` verifies downloads while streaming the response to disk. `FileSystem.Open()` stays a low-level reader; callers that use it directly are responsible for reading the full stream and validating checksums themselves.

#### Open HDFS File
Use the `FileSystem.Open()` to open and read a remote file from HDFS.  
```
data, err := fs.Open(webhdfs.Path{Name:"/remote/file"}, 0, 512, 2048)
...
rcvdData, _ := ioutil.ReadAll(data)
fmt.Println(string(rcvdData))

```

#### Append to File
To append to an existing HDFS file, use `FileSystem.Append()`.  
```
ok, err := fs.Append(
    bytes.NewBufferString("Hello webhdfs users!"),
    webhdfs.Path{Name:"/remote/file"}, 4096)
```

#### Rename File
Use `FileSystem.Rename()` to rename HDFS resources. 
```
ok, err := fs.Rename(webhdfs.Path{Name:"/old/name"}, Path{Name:"/new/name"})
```

#### Delete HDFS Resources
To delete an HDFS resource (file/directory), use `FileSystem.Delete()`.  
```go
ok, err := fs.Delete(webhdfs.Path{Name:"/remote/file/todelete"}, false)
```

#### File Status
You can get status about an existing HDFS resource using `FileSystem.GetFileStatus()`. 

```go
fileStatus, err := fs.GetFileStatus(webhdfs.Path{Name:"/remote/file"})
```
webhdfs returns a value of type FileStatus which is a struct with info about remote file.
```go
type FileStatus struct {
	AccesTime int64
    BlockSize int64
    Group string
    Length int64
    ModificationTime int64
    Owner string
    PathSuffix string
    Permission string
    Replication int64
    Type string
}
```
You can get a list of file stats using `FileSystem.ListStatus()`.
```go
stats, err := fs.ListStatus(webhdfs.Path{Name:"/remote/directory"})
for _, stat := range stats {
    fmt.Println(stat.PathSuffix, stat.Length)
}
```
### FsShell Examples
#### Create the FsShell
To create an FsShell, you need to have an existing instance of FileSystem.
```go
shell := webhdfs.FsShell{FileSystem:fs}
```
#### FsShell.Put()
Use the put to upload a local file to an HDFS file system. 
```go
ok, err := shell.Put("local/file/name", "hdfs/file/path", true)
```
#### FsShell.Get()
Use the Get to retrieve remote HDFS file to local file system. 
```go
ok, err := shell.Get("hdfs/file/path", "local/file/name")
```

#### FsShell.AppendToFile()
Append local files to remote HDFS file or directory. 
```go
ok, err := shell.AppendToFile([]string{"local/file/1", "local/file/2"}, "remote/hdfs/path")
```

#### FsShell.Chown()
Change owner for remote file.  
```go
ok, err := shell.Chown([]string{"/remote/hdfs/file"}, "owner2")
```

#### FsShell.Chgrp()
Change group of remote HDFS files.  
```go
ok, err := shell.Chgrp([]string{"/remote/hdfs/file"}, "superduper")
```

#### FsShell.Chmod()
Change file mod of remote HDFS files.  
```go
ok, err := shell.Chmod([]string{"/remote/hdfs/file/"}, 0744)
```

## Contributing
Contributions are welcome! Please feel free to submit a pull request.

## License
This project is licensed under the MIT License - see the LICENSE file for details.

### Limitations
1. Only "SIMPLE" security mode supported.
2. No support for kerberos (none plan right now)
3. No SSL support yet.

### References
1. WebHDFS API - http://hadoop.apache.org/docs/current/hadoop-project-dist/hadoop-hdfs/WebHDFS.html
2. FileSystemShell - http://hadoop.apache.org/docs/current/hadoop-project-dist/hadoop-common/FileSystemShell.html#getmerge
