# TinyRange (Build System) Starlark API

## Globals

### `json`

#### `json.encode`

`def encode(x)`

The encode function accepts one required positional argument,
which it converts to JSON by cases:

- A Starlark value that implements Go's standard json.Marshal interface defines its own JSON encoding.
- None, True, and False are converted to null, true, and false, respectively.
- Starlark int values, no matter how large, are encoded as decimal integers.
Some decoders may not be able to decode very large integers.
- Starlark float values are encoded using decimal point notation, even if the value is an integer. It is an error to encode a non-finite floating-point value.
- Starlark strings are encoded as JSON strings, using UTF-16 escapes.
- a Starlark IterableMapping (e.g. dict) is encoded as a JSON object. It is an error if any key is not a string.
- any other Starlark Iterable (e.g. list, tuple) is encoded as a JSON array.
- a Starlark HasAttrs (e.g. struct) is encoded as a JSON object.

It an application-defined type matches more than one the cases describe above, (e.g. it implements both Iterable and HasFields), the first case takes precedence. Encoding any other value yields an error.


#### `json.decode`

`def decode(x[, default]):`

The decode function has one required positional parameter, a JSON string. It returns the Starlark value that the string denotes.

- Numbers are parsed as int or float, depending on whether they contain a decimal point.
- JSON objects are parsed as new unfrozen Starlark dicts.
- JSON arrays are parsed as new unfrozen Starlark lists.

If x is not a valid JSON string, the behavior depends on the "default" parameter: if present, Decode returns its value; otherwise, Decode fails.

#### `json.indent`

`def indent(str, *, prefix="", indent="\t"):`

The indent function pretty-prints a valid JSON encoding, and returns a string containing the indented form. It accepts one required positional parameter, the JSON string, and two optional keyword-only string parameters, prefix and indent, that specify a prefix of each new line, and the unit of indentation.

### `db` `PackageDatabase`

`TODO(joshua)`

### `load_fetcher`

`def load_fetcher(filename):`

Load a fetcher from `filename`.

This is similar to `load` except it makes definitions accessible though `tinyrange build`.

### `define`

`TODO(joshua)`

### `directive`

`TODO(joshua)`

### `installer`

`def installer(tags=[], directives=[], dependencies=[]):`

Returns a new `Installer`.

A installer consists of a list of `directives` associated with a list of `tags` to filter it's eligibility. A installer can also include a list of `Query`s as `dependencies` that will be automatically added when the installer is selected.

### `name`

`def name(name, version, tags=[]):`

Returns a package `Name` with `name` name and `version` version and optionally a list of tags `tags`.

### `query`

`def query(name, tags=[]):`

Return a package `Query` parsed from `name` with an optional list of `tags`.

Queries are generally formatted like `name:version`.

Passing `*` for name returns a special `Query` that matches any package.

### `shuffle`

`def shuffle(values):`

Return a copy of the iterable `values` randomly shuffled.

### `sleep`

`def sleep(dur):`

Sleep the current thread for `dur` nanoseconds.

#### Examples

```python
sleep(duration("1s")) # Sleep for 1 second.
```

### `duration`

`def duration(dur):`

Parse a duration string `dur` and return the number of nanoseconds.

A duration string is a possibly signed sequence of decimal numbers, each with optional fraction and a unit suffix, such as "300ms", "-1.5h" or "2h45m".

Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".

#### Examples

```python
sleep(duration("1s")) # Sleep for 1 second.
```

### `filesystem`

`def filesystem(ark=None):`

Create a new `Directory`.

If `ark` is specified and a archive then extract all the entires into the new filesystem.

### `file`

`def file(contents, name="", executable=False):`

Create a new regular file `File`. How depends on what type `contents` is.

- If `contents` is a string then set the contents of the file to that.
- If `contents` is a `File` then copy the contents to the new file.

If `executable` is true then mark the file as executable.

#### Examples

```python
file("Hello, World").read() == "Hello, World"

file(file("Hello")).read() == "Hello"
```

### `error`

`def error(message):`

Raises a fatal error with the given `message`.

### `time`

`def time():`

Returns the number of fractional seconds since TinyRange started.

## Types 

### `PackageDatabases`

`TODO(joshua)`

### `File`

`TODO(joshua)`

### `Directory`

`TODO(joshua)`

### `Query`

`TODO(joshua)`

### `Name`

`TODO(joshua)`

### `Installer`

`TODO(joshua)`