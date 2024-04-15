"""
Common fetcher utilities.
"""

def fetch_github_archive(user, repo, ref = "main"):
    url = "https://github.com/{}/{}/archive/refs/heads/{}.tar.gz".format(user, repo, ref)
    resp = fetch_http(url, fast = True)
    if resp == None:
        return None
    return resp.read_archive(".tar.gz", strip_components = 1)
