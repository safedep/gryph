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

TEXT authorizationCreateTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationCreate(SB)
GLOBL	·authorizationCreateAddr(SB), RODATA, $8
DATA	·authorizationCreateAddr(SB)/8, $authorizationCreateTrampoline<>(SB)

TEXT authorizationCopyRightsTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationCopyRights(SB)
GLOBL	·authorizationCopyRightsAddr(SB), RODATA, $8
DATA	·authorizationCopyRightsAddr(SB)/8, $authorizationCopyRightsTrampoline<>(SB)

TEXT authorizationMakeExternalFormTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationMakeExternalForm(SB)
GLOBL	·authorizationMakeExternalFormAddr(SB), RODATA, $8
DATA	·authorizationMakeExternalFormAddr(SB)/8, $authorizationMakeExternalFormTrampoline<>(SB)

TEXT authorizationCreateFromExternalFormTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationCreateFromExternalForm(SB)
GLOBL	·authorizationCreateFromExternalFormAddr(SB), RODATA, $8
DATA	·authorizationCreateFromExternalFormAddr(SB)/8, $authorizationCreateFromExternalFormTrampoline<>(SB)

TEXT authorizationCopyInfoTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationCopyInfo(SB)
GLOBL	·authorizationCopyInfoAddr(SB), RODATA, $8
DATA	·authorizationCopyInfoAddr(SB)/8, $authorizationCopyInfoTrampoline<>(SB)

TEXT authorizationFreeItemSetTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationFreeItemSet(SB)
GLOBL	·authorizationFreeItemSetAddr(SB), RODATA, $8
DATA	·authorizationFreeItemSetAddr(SB)/8, $authorizationFreeItemSetTrampoline<>(SB)

TEXT authorizationFreeTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationFree(SB)
GLOBL	·authorizationFreeAddr(SB), RODATA, $8
DATA	·authorizationFreeAddr(SB)/8, $authorizationFreeTrampoline<>(SB)

TEXT authorizationRightGetTrampoline<>(SB),NOSPLIT,$0-0
	JMP	gryph_AuthorizationRightGet(SB)
GLOBL	·authorizationRightGetAddr(SB), RODATA, $8
DATA	·authorizationRightGetAddr(SB)/8, $authorizationRightGetTrampoline<>(SB)
