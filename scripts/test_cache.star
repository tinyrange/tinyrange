def make_hello(ctx):
    # Create a new filesystem.
    fs = filesystem()

    # Assigning to a path creates a file. Intermidate directories are
    # automaticly created.
    fs["hello1.txt"] = file("world")

    # The string is automaticly casted to a file. This is syntacticly
    # equivilient to the previous statement.
    fs["hello2.txt"] = "world"

    # Creates a new archive from a filesystem.
    # The filesystem only exists in memory until it is returned as a archive.
    return ctx.archive(fs, format = ".tar")

def main(ctx):
    # build creates a new cache entry with the tag and returns the result.
    # __file__ refers to the filename of current script.
    f = ctx.build((__file__, "hello"), make_hello, ())

    print(get_cache_filename(f))  # needs -allowLocal to print a host filename.

    # Example pulling a file from the archive.
    print(f.read_archive(".tar")["hello1.txt"].read())

if __name__ == "__main__":
    run_script(main)
