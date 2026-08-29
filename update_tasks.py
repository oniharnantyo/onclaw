with open('openspec/changes/reshape-admin-nav-and-tenant-control/tasks.md', 'r') as f:
    lines = f.readlines()

for i in range(len(lines)):
    if lines[i].startswith("- [ ] 7.1"):
        lines[i] = lines[i].replace("- [ ] 7.1", "- [x] 7.1")
    elif lines[i].startswith("- [ ] 7.2"):
        lines[i] = lines[i].replace("- [ ] 7.2", "- [x] 7.2")
    elif lines[i].startswith("- [ ] 7.3"):
        lines[i] = lines[i].replace("- [ ] 7.3", "- [x] 7.3")
    elif lines[i].startswith("- [ ] 7.5"):
        lines[i] = lines[i].replace("- [ ] 7.5", "- [x] 7.5")

with open('openspec/changes/reshape-admin-nav-and-tenant-control/tasks.md', 'w') as f:
    f.writelines(lines)
