# The PiVo Repo

> "*When you are in scalable REST world, there is not much else than JSON with binary blobs being published through dumb CDNs.
> 
> With PVR, consuming and sharing file trees in json format becomes a joy for REST environments.*"

# Features

 * simple repository in json format with object store
 * checkout and commit any working directory to repository
 * look at working tree diffs with `status` and `diff` commands
 * get, push and apply patches as incremental updates
 * all repo operations are atomic and can be recovered
 * object store can be local or in cloud/CDN

# Install

1. Get latest pvr from your distribution, e.g.

    ```
    $ apt-get install pvr
    ```

2. Download latest binary matching your architecture:

    ```
    $ wget http://downloads.pantahub.com/x86-64/pvr
    $ chmod a+x pvr
    ```

# Get Started

pvr is about transforming a directory structure that contains files as well as json files into
a single managable, diffable and mergeable json file that unambiguously defines directory state.

To leverage the features of json diff/merge etc. all json files found in a directory tree will
get inlined while all other objects will get a sha reference entry with the files themselves
getting stored in a flat objects directory.

# Basics

To start a pvr from scratch you use the pvr init command which sets you up.

```
example1$ pvr init
pvc directory ready for use.
```

However, more likely is that you want to consume an existing pvr made by you
or someone else and maybe change things against it:

```
pvr clone /path/to/pvr/repo example2
 -> mkdir example2
 -> pvr init
 -> pvr get /path/to/pvr/repo
 -> pvr checkout
```

While working on changes to your local checkout, you can use `status` and `diff`
to observe your current changes:

```
example2$ ../pvr status
A newfile.txt
D deleted.txt
C some.json
C working.txt
```

This means that `newfile.txt` is new, working.txt and some.json changed and
`deleted.txt` got removed from your working directory.

You can introspect the changes through the `diff` command:

```
example2$ ../pvr diff
{
	"deleted.txt": null,
	"newfile.txt": "dc460da4ad72c482231e28e688e01f2778a88ce31a08826899d54ef7183998b5",
	"some.json": {
		"values": "2"
	},
	"working.txt": "9c7ab50fa91f3d78744043af5f88dce6bacd336f47733ff6a38090da3ff1a5de"
}
```

Being happy with what you see, you can checkpoint your working state using the
`commit` command:

```
example2$ ../pvr commit
Committing some.json
Committing working.txt
Adding newfile.txt
Removing deleted.txt
```
This will atomically update the json file in your repo after ensuring all the
right objects have been synched into the objects store of that pvr repo.

After committing your changes you might want to store your current repository
state for reuse or archiving purpose. You can do so using the `push` command:

```
example2$ pvr push /tmp/myrepo
```

You can also push your repository to a pvr compliant REST backend.

```
example2: pvr push https://pantahub.com/pvr/v1/my-repo1
```

You can later clone that very repo to use it as a starting point or get 
its content to update another repo.


# Internals

The pvr repository has the following structure in v1:

objects/:
```
ls objects/
8862f6feea4f6d01e28adc674285640874da19d7594dd80ed42ff7fb4dc0eea3
ad6da30bb62fae51c770574a5ca33c5e8e4bbc67fd6c5e7c094c34ad52a28e4d
d0365cf6153143a414cccaca9260bc614593feba9fe0379d0ffb7a1178470499
d9206603679fcf0a10bf4e88bf880222b05b828749ea1e2874559016ff0f5230
```

json:
```
cat json
{
  "spec": "pantavisor-multi-platform@1",
  "brcm.tar.gz": "8862f6feea4f6d01e28adc674285640874da19d7594dd80ed42ff7fb4dc0eea3",
  "pp/test.txt": "ad6da30bb62fae51c770574a5ca33c5e8e4bbc67fd6c5e7c094c34ad52a28e4d",
  "pp/test1.txt": "ad6da30bb62fae51c770574a5ca33c5e8e4bbc67fd6c5e7c094c34ad52a28e4d",
  "test.json": {
    "I": [
      "thank",
      "you"
    ],
    "My": "Mother",
    "more": "than"
  }
}
```

Cloud Restendpoints are expected to have an objects endpoint in parallel that will resolve the right GET and PUT Urls for a requested resource.

The client will request objects like:

```http GET http://someurl.tld/path/to/json/parent/objects/:id```

The server is supposed to either deliver the object or redirect to the right location.

# Commands

## pvr init <SPEC>
```
$ pvr init pantavisor-multi-platform@1
Created empty pantavisor-multi-platform@1 project.

$ cat .pvr/system.json
{
	"#spec": "pantavisor-multi-platform@1",
	"systemc.json" {
		"linux": "",
		"initrd": [
			"",
		],
		"platforms:": [],
		"volumes": {},
	}
}
```

As you can see the init program has created a template for you
that needs filling up.

```
$ ls -a
.
..
.pvr
systemc.json
```

You would now continue editing this directory as it pleases you. and you can refer to any file you put here in your configs just using the absolute path (e.g. /systemc.json).

## pvr add [file1 file2 ...]

`prv add` will put a file that exists in working directory under management of pvr. This means that the file will be honored on future `pvr diff` and `pvr commit` operations.

Example: If you bring in a basic platform to your system you simply copy them into the working dir and put them under pvr management:

```
$ cp /from/somewhere.conf lxc-platform.conf
$ cp /from/somewhere.json lxc-platform.json
$ pvr add lxc-platform.json pxc-platform.conf
```

These files will then be part of the next commit.

## pvr diff
You can look at your current changes to working directory using the diff command to get RFCXXXX json patch format:

```
$ pvr diff
{
	"lxc-platform.conf": "sha1:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
	"lxc-platform.conf": "sha1:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
}
```

## pvr commit
Committing your pvr will update the .pvr directory so it can be pushed to pantahub.

```
$ pvr commit -m "my commit message"
Adding file xxx
Changing File yyy
Commit Done
```

You can then continue editing and see your changes compared to the committed baseline using `pvr diff` again.

## pvr push <destination>

Push local:
```
$ pvr push /some/local/repopath
[=============================================] 100%
$ find /some/local/repopath
/some/local/repopath/json
/some/local/repopath/rev
/some/local/repopath/commitmsg
/some/local/repopath/objects/sha1:xxxxxxxxxxxxxxxxxxxxxx
/some/local/repopath/objects/sha1:yyyyyyyyyyyyyyyyyyyyyy
```

Push to your device:
```
$ pvr push https://localhost:12366/pvr/api/v1/DEVICEID/system
DONE [=============================================] 100%
```

Talk about your device on panta blog:

```
$ pvr post -h https://plog.pantahub.com/ricmm/
Title: My first raspberry pi system!
Tags: rpi3 yocto minimal system
Summary: Install this following the instructions and
  post your comments through disqus.
Instructions:
# MD Format Instructions
here.
<EMPTYLINE>
<EMPTYLINE>
Posted: https://plog.pantahub.com/ricmm/my-first-raspberry-pi-system
$
```

## pvr get <LOCATION> (Blog)

A reader wants to use your first raspberry pi system and apply it to one of his rpi3s

```
$ pvr get https://plog.pantahub.com/ricmm/my-first-raspberry-pi-system
DONE [=============================================] 100%
$ pvr push https://localhost:12366/pantavisor/api/v1/DEVICEID/system
DONE [=============================================] 100%
```

## pvr get <LOCATION> (Device)

A developer or admin or app wants to get state from a device instead of a post. He can also do that using `pvr get` primitive:
```
$ pvr get https://localhost:12366/pantavisor/api/v1/DEVICEID/system
```

```
$ pvr get https://localhost:12366/pantavisor/api/v1/DEVICEID1/system#latest
DONE [=============================================] 100%
$ pvr push https://localhost:12366/pantavisor/api/v1/DEVICEID2/system
DONE [=============================================] 100%
```


# References

## Example pvr json

```
"pvr":
{
	"#spec": "pantavisor-multi-platform@1",
	"myvideo.blob": "sha:xxxxxxxxxxxxxxxxxxxxxxxxxxx",
	"config.json" {
	  "key": "value"
	},
	"conf/lxc-owrt-mips.conf": "sha:tttttttttttttttttttttt",
	"conf/lxc-ble-gw1.conf": "sha:rrrrrrrrrrrrrrrrrrrr",
	"systemc.json" {
		"linux": "/kernel.img",
		"initrd": [
			"/0base.cpio.gz",
			"/asacrd.cpio.gz",
		],
		"platforms:": ["lxc-owrt-mips.json"]
		"volumes": [],
		},
	"lxc-owrt-mips.json":
	{
		"spec": "pantavisor-lxc-runner@1",
		"parent": null,
		"lxc-config": "/owrt.json",
		"lxc-shares": [ NETWORK, UTS, IPC ],
		"lxc-exec": "/init"
	},
	"lxc-azure-ble-gw1.json":
	{
		"parent": lxc-owrt-mips,
		"runner": "lxc",
		"lxc-config": "/files/lxc-ble-gw1.conf",
		"lxc-shares": [],
		"lxc-exec": "/init"
	},
}
```