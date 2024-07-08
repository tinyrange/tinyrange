load_fetcher("//fetchers/alpine.star")

def main(args):
    for pkg in db.builder("alpine@3.20").packages:
        installer = pkg.installer_for(["level3"])[0]
        for directive in installer.directives:
            if type(directive) == "BuildDefinition":
                db.build(directive)
