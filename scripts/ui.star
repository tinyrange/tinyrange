supervisor_conf = directive.add_file(
    "/root/supervisord.conf",
    file("""[supervisord]
logfile=/root/supervisord.log
user=root

[program:xvnc]
command=/usr/bin/Xvnc :1 -once -SecurityTypes=None
autorestart=true
user=root
priority=100
stdout_logfile=/root/xvnc.log
stdout_logfile_maxbytes=0
stderr_logfile=/root/xvnc.log
stderr_logfile_maxbytes=0

[program:session]
environment=HOME="/root",DISPLAY=":1",USER="root"
command=/root/Xsession
user=root
autorestart=true
priority=300
stdout_logfile=/root/session.log
stdout_logfile_maxbytes=0
stderr_logfile=/root/session.log
stderr_logfile_maxbytes=0
"""),
)

ui_script = directive.run_command("""
source /etc/profile

/usr/bin/supervisord -c "/root/supervisord.conf" -n &
        
sleep 2
        
tail -f /root/*.log &
        
sleep infinity        
""")

base_packages_alpine = [
    query("openrc"),
    query("tigervnc"),
    query("supervisor"),
    query("dbus"),
    query("dbus-x11"),
    query("dbus-openrc"),
]

xfce4_packages = [
    query("xfce4"),
    query("xfce4-terminal"),
    query("adwaita-icon-theme"),
    query("faenza-icon-theme"),
]

xfce4 = directive.list([
    directive.add_package(pkg)
    for pkg in base_packages_alpine + xfce4_packages
] + [
    directive.add_file(
        "/root/Xsession",
        file("#!/bin/sh\nexec /usr/bin/xfce4-session"),
        executable = True,
    ),
    directive.add_file("/etc/network/interfaces", file("auto lo")),
    directive.run_command("openrc"),
    directive.run_command("touch /run/openrc/softlevel"),
    directive.run_command("service dbus start"),
    supervisor_conf,
    ui_script,
    directive.interaction("vnc"),
])
