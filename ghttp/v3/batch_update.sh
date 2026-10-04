#!/bin/bash
# 批量修改测试文件，将 req.Data.Field 改为 data, _ := req.Data(ctx); data.Field

# 修改 api_test.go
sed -i '71,76s/func(_ context.Context, req RequestOf\[/func(ctx context.Context, req RequestOf[/g' api_test.go
sed -i '72s/if req\.Data == nil {/data, err := req.Data(ctx); if err != nil { return "", err }; if data == nil {/g' api_test.go
sed -i '75s/return req\.Data\.Name, nil/return data.Name, nil/g' api_test.go

sed -i '77s/func(_ context.Context, req RequestOf\[/func(ctx context.Context, req RequestOf[/g' api_test.go
sed -i '77s/return req\.Data\.ID + req\.Data\.Page, nil/data, _ := req.Data(ctx); return data.ID + data.Page, nil/g' api_test.go

# 修改其他文件继续...
echo "Batch update completed"
