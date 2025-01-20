#!/usr/bin/env python3

from absl import app, logging, flags
from os import path
from subprocess import check_call

FILE_DIR = path.dirname(path.abspath(__file__))


def parse_type(raw: str):
    if raw.endswith("]"):
        name, typ = raw.removesuffix("]").split("[")

        if name == "qid" and typ == "13":
            return (name, "QID")
        elif name == "aqid" and typ == "13":
            return (name, "QID")
        elif typ == "1":
            return (name, "uint8")
        elif typ == "2":
            return (name, "uint16")
        elif typ == "4":
            return (name, "uint32")
        elif typ == "8":
            return (name, "uint64")
        elif typ == "s":
            return (name, "string")
        elif typ == "count":
            return (name, "[]byte")
        elif typ == "Dirent":
            return (name, "[]Dirent")
        else:
            raise NotImplementedError("name={}, typ={}".format(name, typ))
    elif raw == "nwname*(wname[s])":
        return "nwname", "[]string"
    elif raw == "nwqid*(wqid[13])":
        return "nwqid", "[]QID"
    else:
        raise NotImplementedError(raw)


def get_encoder(name: str, typ: str) -> str:
    name = make_go_name(name)

    if typ == "uint8":
        return "r.Uint8(t.{})".format(name)
    elif typ == "uint16":
        return "r.Uint16(t.{})".format(name)
    elif typ == "uint32":
        return "r.Uint32(t.{})".format(name)
    elif typ == "uint64":
        return "r.Uint64(t.{})".format(name)
    elif typ == "string":
        return "writeString(r, t.{})".format(name)
    elif typ == "QID":
        return "if err := t.{}.Encode(r); err != nil".format(name) + " { return err }"
    elif typ == "[]byte":
        return "r.Bytes(t.{})".format(name)
    elif typ == "[]QID":
        return (
            "r.Uint16(uint16(len(t.{})));for _, q := range t.{}".format(name, name)
            + "{ if err := q.Encode(r); err != nil { return err } }"
        )
    elif typ == "[]Dirent":
        return (
            "for _, d := range t.{}".format(name)
            + "{ if err := d.Encode(r); err != nil { return err } }"
        )
    elif typ == "time.Time":
        return (
            "r.Uint64(uint64(t.{}.Unix()));r.Uint64(uint64(t.{}.Nanosecond()))".format(
                name, name
            )
        )
    else:
        raise NotImplementedError("name={}, typ={}".format(name, typ))


def get_decoder(name: str, typ: str) -> str:
    name = make_go_name(name)

    if typ == "uint8":
        return "t.{} = r.Uint8()".format(name)
    elif typ == "uint16":
        return "t.{} = r.Uint16()".format(name)
    elif typ == "uint32":
        return "t.{} = r.Uint32()".format(name)
    elif typ == "uint64":
        return "t.{} = r.Uint64()".format(name)
    elif typ == "string":
        return "t.{} = readString(r)".format(name)
    elif typ == "[]byte":
        return "t.{} = r.Bytes(int(t.Count))".format(name)
    elif typ == "[]string":
        return (
            "count := r.Uint16();for i := 0; i < int(count); i += 1 {"
            + "t.{} = append(t.{}, readString(r))".format(name, name)
            + "}"
        )
    elif typ == "time.Time":
        return "t.{} = time.Unix(int64(r.Uint64()), int64(r.Uint64()))".format(name)
    else:
        raise NotImplementedError("name={}, typ={}".format(name, typ))


def make_go_name(name: str) -> str:
    return "".join([s.title() for s in name.split("_")])


def main(args):
    statement_list = []
    type_assertions = []

    with open(path.join(FILE_DIR, "protocol.txt"), "r") as f:
        for line in f:
            name, *elements = line.strip().split(" ")

            elements = [parse_type(e) for e in elements]

            optimized_elements = []

            for i, (var_name, typ) in enumerate(elements):
                if i == 0 and var_name == "tag":
                    continue
                elif elements[i][0].endswith("_sec") and elements[i + 1][0].endswith(
                    "_nsec"
                ):
                    optimized_elements.append(
                        (var_name.removesuffix("_sec"), "time.Time")
                    )
                elif var_name.endswith("_nsec"):
                    continue
                elif i + 1 < len(elements) and elements[i + 1][0] == var_name:
                    continue
                else:
                    optimized_elements.append((var_name, typ))

            logging.info("name = %s, elements = %s", name, optimized_elements)

            statement_list.append(
                "type {} struct {}".format(
                    name,
                    "{"
                    + "\n".join(
                        [
                            "{} {}".format(make_go_name(name), typ)
                            for name, typ in optimized_elements
                        ]
                    )
                    + "}",
                )
            )

            if name.startswith("T"):
                decode_body = "{\n"

                for var_name, typ in optimized_elements:
                    decode_body += get_decoder(var_name, typ) + "\n"

                decode_body += "return r.Error()\n"

                decode_body += "}\n"

                statement_list.append(
                    "func (t *{}) Decode(r binary.BinaryReader) error".format(name)
                    + decode_body
                )

                type_assertions.append("_ binary.Decodable = &" + name + "{}")
            elif name.startswith("R") or name == "Dirent":
                encode_body = "{\n"

                for var_name, typ in optimized_elements:
                    encode_body += get_encoder(var_name, typ) + "\n"

                encode_body += "return r.Error()\n"

                encode_body += "}\n"

                statement_list.append(
                    "func (t *{}) Encode(r binary.BinaryWriter) error".format(name)
                    + encode_body
                )

                type_assertions.append("_ binary.Encodable = &" + name + "{}")

    with open(path.join(FILE_DIR, "protocol_gen.go"), "w") as f:
        f.write(
            """// autogenerated by gen_protocol.py
package p9

import (
    "time"
                
    "github.com/tinyrange/tinyrange/pkg/common/binary"
)
"""
        )

        f.write("\n\n".join(statement_list))

        f.write("\n\n")

        f.write("var (\n" + "\n".join(type_assertions) + "\n)\n")

    check_call(["go", "fmt", path.join(FILE_DIR, "protocol_gen.go")])


if __name__ == "__main__":
    app.run(main)
