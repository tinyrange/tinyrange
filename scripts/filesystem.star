def main():
    fs = filesystem()
    fs["hello.txt"] = file("Hello, World", executable = True)

    fs["testing/hello2.txt"] = file("Hello, World 2")

    for k in fs:
        print(k)
