# Systemd configuration

When you're just starting to use mdmdirector on a server, you might run

```shell
mdmdirector -cert '/path/to/certificate' \
    -dbconnection 'host=127.0.0.1 port=5432 user=postgres dbname=postgres password=password sslmode=disable' \
    -micromdmapikey 'supersecret' \
    -micromdmurl 'https://mdm.acme.com' \
    -password 'supersecret'
```

and follow the output directly. But as soon as you close your terminal/ssh session the server will stop running.

A standard way to run services on linux is using [`systemd`](https://coreos.com/os/docs/latest/getting-started-with-systemd.html). Systemd has a number of benefits, but mainly:

- it will keep your process running.
- it will restart your process after a failure.
- it will restart your process after a server restart.
- it will pass the logs written to stout/stderr to `journalctl` or `syslog`.

Getting started is easy.
First, create a file like called `mdmdirector.service` on your linux host.

```shell
[Unit]
Description=mdmdirector MDM Orchastration Server
Documentation=https://github.com/mdmdirector/mdmdirector
After=network.target

[Service]
ExecStart=/usr/local/bin/mdmdirector  -cert '/path/to/certificate' \
    -dbconnection 'host=127.0.0.1 port=5432 user=postgres dbname=postgres password=password sslmode=disable' \
    -micromdmapikey 'supersecret' \
    -micromdmurl 'https://mdm.acme.com' \
    -password 'supersecret'
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Note that the `ExecStart` should have the `mdmdirector` command with the configuration appropriate for your server. Change/add/remove CLI flags as needed, but keep them with the `ExecStart=` line.

Once you created the file, you need to move it to `/etc/systemd/system/mdmdirector.service` and start the service.

```shell
sudo mv mdmdirector.service /etc/systemd/system/mdmdirector.service
sudo systemctl start mdmdirector.service
sudo systemctl status mdmdirector.service
```

If your `ExecStart` line is all correct you should see the service running.

Use `sudo journalctl -u mdmdirector.service -f` to tail the server logs.

## Making changes

Sometimes you'll need to update the systemd unit file defining the service. To do that, first open `/etc/systemd/system/mdmdirector.service` in a text editor, and apply your changes.

Then, run

```shell
sudo systemctl daemon-reload
sudo systemctl restart mdmdirector.service
```

## References

- https://coreos.com/os/docs/latest/getting-started-with-systemd.html
- https://www.digitalocean.com/community/tutorials/systemd-essentials-working-with-services-units-and-the-journal
- https://www.freedesktop.org/software/systemd/man/systemd.service.html

## Showing mdmdirector logs using journalctl

```shell
sudo journalctl -u mdmdirector.service -f
```

### Credit

The following was originally found on the [MicroMDMWiki](https://github.com/micromdm/micromdm/wiki/Using-MicroMDM-with-systemd) original contributions by [Groob](https://github.com/groob) & [Dan Falzon](https://github.com/danfalzon-sohonet)
