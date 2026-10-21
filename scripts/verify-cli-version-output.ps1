# Copyright 2026 Ayesh Almeida
# Licensed under the Apache License, Version 2.0.
param(
    [Parameter(Mandatory=$true)][string]$Output,
    [Parameter(Mandatory=$true)][string]$ExpectedCliVersion,
    [Parameter(Mandatory=$true)][string]$ExpectedCoreVersion
)

$expected = "komposyn-lg $ExpectedCliVersion (core $ExpectedCoreVersion)"
if ($Output -cne $expected) {
    throw "expected executable metadata '$expected'; got '$Output'"
}
