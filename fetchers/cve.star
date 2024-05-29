"""
This fetcher implementation is not ideal.

Best practices (from https://nvd.nist.gov/developers/start-here) is to populate
initally by doing a bulk download then using the date modified fields to update
your cache.

This code instead downloads blocks of modified CVEs according to the day there
were last modified. This leads to sending a lot more requests to the server.
Each response is normally expected to contain 2000 results but most days only
have ~400 changes.
"""

API_ENDPOINT = "https://services.nvd.nist.gov/rest/json/cves/2.0"

def fetch_cve_infomation(ctx, year, month):
    start = "{}-{}-01T00:00:00.000".format(year, "0" + str(month) if month < 10 else str(month))

    month += 1
    if month > 12:
        month = 1
        year += 1

    end = "{}-{}-01T00:00:00.000".format(year, "0" + str(month) if month < 10 else str(month))

    resp = fetch_http(
        API_ENDPOINT,
        params = {
            "lastModStartDate": start,
            "lastModEndDate": end,
        },
        wait_time = duration(seconds = 10),
        expire_time = duration(hours = 24 * 365),  # Expire downloads after a year.
    )

    resp = json.decode(resp.read())

    vulns = resp["vulnerabilities"]

    start_index = resp["resultsPerPage"]

    for _ in range(100):
        if start_index >= resp["totalResults"]:
            break

        resp = fetch_http(
            API_ENDPOINT,
            params = {
                "lastModStartDate": start,
                "lastModEndDate": end,
                "startIndex": str(start_index),
            },
            wait_time = duration(seconds = 10),
            expire_time = duration(hours = 24 * 365),  # Expire downloads after a year.
        )

        resp = json.decode(resp.read())

        vulns += resp["vulnerabilities"]

        start_index += resp["resultsPerPage"]

    for vuln in vulns:
        if vuln["cve"]["vulnStatus"] == "Rejected":
            continue
        pkg = ctx.add_package(ctx.name(
            name = vuln["cve"]["id"],
        ))
        pkg.set_raw(json.encode(vuln))
        description = [k["value"] for k in vuln["cve"]["descriptions"] if k["lang"] == "en"][0]
        pkg.set_description(description)

    return None

if __name__ == "__main__":
    for year in range(2010, 2024):
        for month in range(1, 13):
            fetch_repo(fetch_cve_infomation, (year, month), distro = "cve")
