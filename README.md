# Apple idToken validate
```
go get github.com:xsean2020/apple

import "github.com:xsean2020/apple/idtoken"

payload , err := idtoken.Validate(context.TODO(), idToken, "")
checkErr(err)
log.Println(paload.Identify())

```
