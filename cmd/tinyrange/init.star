def main():
    network_interface_up("lo")
    network_interface_up("eth0")
    network_interface_configure("eth0", ip = "10.42.0.2/16", router = "10.42.0.1")

    contents = fetch_http("http://10.42.0.1/hello")

    print(contents)

    run("/bin/login", "-f", "root")
