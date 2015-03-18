# iperf

## Notes

### Parallel Streams

- spawn N clients, each with a results channel, terminator, and interval timer
- collect intermediate results from each client
- add results together and print sum
