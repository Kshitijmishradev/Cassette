package commands

import "runtime"

func numCPU() int { return runtime.NumCPU() }
