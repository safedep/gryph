//go:build darwin

#include "textflag.h"

// Each trampoline jumps to a C function that the dynamic linker binds.
// Go code reads the address of a trampoline from its Addr variable.

TEXT freeTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_free(SB)
GLOBL	·freeAddr(SB), RODATA, $8
DATA	·freeAddr(SB)/8, $freeTrampoline<>(SB)

TEXT launchActivateSocketTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_launch_activate_socket(SB)
GLOBL	·launchActivateSocketAddr(SB), RODATA, $8
DATA	·launchActivateSocketAddr(SB)/8, $launchActivateSocketTrampoline<>(SB)
