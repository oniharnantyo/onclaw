import re

with open('./scripts/smoke.sh', 'r') as f:
    content = f.read()

content = content.replace('"{"name":"Renamed Smoke Tenant", "timezone":"Europe/London"}"', "'{\"name\":\"Renamed Smoke Tenant\", \"timezone\":\"Europe/London\"}'")
content = content.replace('"{"email":"${CHARLIE_EMAIL}","role_id":"${MEMBER_ROLE_ID}"}"', '"{\\"email\\":\\"${CHARLIE_EMAIL}\\",\\"role_id\\":\\"${MEMBER_ROLE_ID}\\"}"')
content = content.replace('"{"email":"${CLI_USER_EMAIL}","role_id":"${OWNER_ROLE_ID}"}"', '"{\\"email\\":\\"${CLI_USER_EMAIL}\\",\\"role_id\\":\\"${OWNER_ROLE_ID}\\"}"')
content = content.replace('"{"email":"nobody@nowhere.test","role_id":"${MEMBER_ROLE_ID}"}"', '"{\\"email\\":\\"nobody@nowhere.test\\",\\"role_id\\":\\"${MEMBER_ROLE_ID}\\"}"')
content = content.replace('"{"user_id":"${CHARLIE_UID}"}"', '"{\\"user_id\\":\\"${CHARLIE_UID}\\"}"')

with open('./scripts/smoke.sh', 'w') as f:
    f.write(content)
