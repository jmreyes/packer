---
layout: "docs"
page_title: "regexreplace - Functions - Configuration Language"
sidebar_current: "configuration-functions-string-regexreplace"
description: |-
  The regexreplace function searches a given string for another given substring,
  and replaces all occurrences with a given replacement string. The substring
  argument can be a valid regular expression or a string.
---

# `regexreplace` Function

`regexreplace` searches a given string for another given substring, and
replaces each occurrence with a given replacement string. The substring
argument can be a valid regular expression or a string.

```hcl
regexreplace(string, substring, replacement)
```

`substring` should not be wrapped in forward slashes, it is always treated as a
regular expression. The `replacement` string can incorporate captured strings
from the input by using an `$n` or `${n}` sequence, where `n` is the index or
name of a capture group.

## Examples

```
> regexreplace("hello world", "world", "everybody")
hello everybody


> regexreplace("hello world", "w.*d", "everybody")
hello everybody

> regexreplace("-ab-axxb-", "a(x*)b", "$1W)
---

> regexreplace("-ab-axxb-", "a(x*)b", "${1}W")
-W-xxW-
```

## Related Functions

- [`replace`](./replace.html) searches a given string for another given
  substring, and replaces all occurrences with a given replacement string.
