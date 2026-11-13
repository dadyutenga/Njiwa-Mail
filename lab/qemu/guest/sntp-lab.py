#!/usr/bin/env python3
"""Minimal SNTP server for the isolated lab (stands in for pool.ntp.org). It serves this VM's clock, which is itself
only as good as the host clock: it proves the managed host's time-sync check runs, not that NTP works on the internet."""
import socket, struct, time
EPOCH = 2208988800
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.bind(("10.77.0.20", 123))
def ts(t): return struct.pack("!II", int(t) + EPOCH, int((t % 1) * 2**32))
while True:
    data, addr = s.recvfrom(512)
    if len(data) < 48: continue
    now = time.time()
    orig = data[40:48]
    pkt = struct.pack("!BBBb", (0 << 6) | (4 << 3) | 4, 2, 6, -20) + struct.pack("!II", 0, 0) + b"LAB\0" + ts(now) + orig + ts(now) + ts(now)
    s.sendto(pkt, addr)
