def opt(d, key, default = ""):
    if key in d:
        return d[key]
    else:
        return default

def split_maybe(s, split, count, default = ""):
    ret = []

    if s != None:
        tokens = s.split(split, count - 1)
        for tk in tokens:
            ret.append(tk)
        for _ in range(count - len(tokens)):
            ret.append(default)
    else:
        for _ in range(count):
            ret.append(default)

    return ret

def split_dict_maybe(d, key, split):
    if key in d:
        return d[key].split(split)
    else:
        return []
