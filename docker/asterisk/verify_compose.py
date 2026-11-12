"""Check actual normalized Compose output, without printing secrets or paths."""
import ipaddress
import json
import re
from pathlib import Path
import sys


def require(condition):
    if not condition:
        raise ValueError("unsafe Compose configuration")


def verify(config, profile):
    if profile not in ("internal", "host", "vm", "calls", "controls", "hold", "transfer"):
        raise ValueError("unknown profile")
    secret_root = Path("artifacts/asterisk-lab").resolve()
    services = config["services"]
    if profile in ("host", "vm"):
        require(set(services) == {"pbx"})
    if profile in ("internal", "calls", "controls", "hold", "transfer"):
        require(set(services) == ({"pbx", "peer", "test-runner"} if profile == "internal" else {"pbx", "test-runner"}))
        require(config["networks"]["isolated"]["internal"])
    if profile in ("calls", "controls", "hold", "transfer"):
        runner = {"calls":"g0-calls", "controls":"g0-controls", "hold":"g0-hold", "transfer":"g0-transfer"}[profile]
        require(services["pbx"]["environment"]["LAB_VERIFY_CONTACT"] == "1")
        require(services["test-runner"]["image"] == f"rust-microsip-{runner}:0.1.0-lab.1")
        require(services["test-runner"]["build"]["target"] == f"{runner}-runner")
        require(services["test-runner"].get("command") in (None, []))
    for name, service in services.items():
        require(service["read_only"] and service["cap_drop"] == ["ALL"])
        require("no-new-privileges:true" in service["security_opt"])
        require(not service.get("privileged") and not service.get("devices"))
        require(service.get("network_mode") != "host")
        require(re.fullmatch(r"[1-9][0-9]*:[0-9]+", service["user"]) is not None)
        require(not service.get("cap_add") and not service.get("volumes_from"))
        require(service.get("pid") != "host" and service.get("ipc") != "host")
        require(len(service["volumes"]) == 1)
        mount = service["volumes"][0]
        expected = secret_root / ("pbx" if name == "pbx" else "clients")
        require(Path(mount["source"]) == expected)
        require(mount["target"] == "/lab" and mount["read_only"])
        # Compose 2.26 (Debian 13, the lab VM) leaves a false value out; a true one is always there.
        require(not mount["bind"].get("create_host_path", False))
        if profile in ("internal", "calls", "controls", "hold", "transfer"):
            require(not service.get("ports") and set(service["networks"]) == {"isolated"})
    if profile == "host":
        pbx = services["pbx"]
        require(set(pbx["networks"]) == {"host-lab"})
        require(not config["networks"]["host-lab"].get("internal", False))
        require(pbx["environment"]["LAB_PROFILE"] == "host")
        ports = {(p["host_ip"], str(p["published"]), str(p["target"]), p["protocol"]) for p in pbx["ports"]}
        # Compose expands ranges into individual ports in its canonical JSON.
        expected = {("127.0.0.1", "15060", "5060", p) for p in ("tcp", "udp")}
        expected.add(("127.0.0.1", "15061", "5061", "tcp"))
        expected.update(("127.0.0.1", str(p), str(p), "udp") for p in range(10000, 10020))
        require(ports == expected)


    if profile == "vm":
        pbx = services["pbx"]
        require(set(pbx["networks"]) == {"vm-lab"})
        require(not config["networks"]["vm-lab"].get("internal", False))
        require(pbx["environment"]["LAB_PROFILE"] == "vm")
        # One private address, never a wildcard, loopback or public one: the auxiliary VM's.
        bind = ipaddress.IPv4Address(pbx["environment"]["LAB_BIND_IP"])
        require(bind.is_private and not (bind.is_loopback or bind.is_unspecified or bind.is_link_local))
        ports = {(p["host_ip"], str(p["published"]), str(p["target"]), p["protocol"]) for p in pbx["ports"]}
        expected = {(str(bind), "5060", "5060", p) for p in ("tcp", "udp")}
        expected.add((str(bind), "5061", "5061", "tcp"))
        expected.update((str(bind), str(p), str(p), "udp") for p in range(10000, 10020))
        require(ports == expected)


if __name__ == "__main__":
    try:
        verify(json.load(sys.stdin), sys.argv[1])
    except (AssertionError, ValueError, KeyError, IndexError, TypeError):
        print("Compose safety contract rejected", file=sys.stderr)
        sys.exit(1)
    print("Compose safety contract OK (not media/VM ACL proof)")
