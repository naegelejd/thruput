# iperf

## Notes

### Formatting

- manually specify units (kbps, mbps, gbps, KB/s, MB/s, GB/s)
- automically determine rate units
- [KMG]byte count units always automatically determined

### Parallel Streams

- spawn N clients, each with a results channel, terminator, and interval timer
- collect intermediate results from each client
- add results together and print sum
