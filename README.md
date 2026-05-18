#The implement of fdvss-1 and fdpss

##Operating environment
- **Go version**：at least Go 1.19.

```bash
make test
```


## Environment Variable Configuration


###`RNG_MODE`: Sets the random number generator mode. If unset or set to `crypto`, the system uses `crypto/rand` for secure randomness. If set to `fast`, it uses `math/rand`.
###`RNG_SEED`: Sets the random seed. This is only applicable in `fast` mode. It accepts a decimal `int64` integer. If unset, `fast` mode will use the default seed.
###`N`: Sets the committee size. It must be a positive integer where N ≥ 4.

##Command-Line Flag Configuration

###`-field-p`: Specifies the finite field prime P. This parameter is mandatory and must be non-zero. The value must be a prime number and must satisfy the implementation constraint where $(P-1)^2$ does not overflow a uint64 multiplication.


## Directory Structure Overview

###`communication/` : Contains communication layer abstractions, currently implemented primarily as in-memory fake channels for local testing.
###`msgpack/`: Contains serialization helper utilities used for message encoding and decoding.
###`primitives/` : Common Params (VSS threshold parameters N, D) at the protocol layer.
###`protocols/`: The core protocol implementation directory, including FDVSS and FDPSS.


