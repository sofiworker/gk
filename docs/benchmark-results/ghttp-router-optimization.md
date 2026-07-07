# ghttp Router Optimization Benchmark Results

## Environment

```text
goos: windows
goarch: amd64
pkg: github.com/sofiworker/gk/ghttp
cpu: AMD Ryzen 7 8845HS w/ Radeon 780M Graphics
```

## Commands

```powershell
go test ./ghttp -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=5
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=5
```

All commands exited with status 0.

## Focused Radix Lookup

Pure `RadixRouter.lookup` stayed allocation-free across all cases. The 8192-route cases from the final run:

```text
BenchmarkRadixRouterLookup/routes=8192/static-16       	36179776	        35.37 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/static-16       	34404655	        36.66 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/static-16       	33924178	        38.32 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/static-16       	33987208	        40.33 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/static-16       	33407107	        40.00 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/param-16        	11866442	       106.8 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/param-16        	11278406	       104.7 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/param-16        	11553146	       105.3 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/param-16        	13725704	        87.16 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/param-16        	14628454	        87.14 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/deep-param-16   	 6569017	       183.7 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/deep-param-16   	 6529911	       195.0 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/deep-param-16   	 5087271	       222.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/deep-param-16   	 5126324	       211.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/deep-param-16   	 6482431	       182.3 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/wildcard-16     	15015202	        84.98 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/wildcard-16     	13763266	        86.30 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/wildcard-16     	14159241	        85.21 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/wildcard-16     	14302195	        85.68 ns/op	       0 B/op	       0 allocs/op
BenchmarkRadixRouterLookup/routes=8192/wildcard-16     	14549457	        84.76 ns/op	       0 B/op	       0 allocs/op
```

## Router Implementation Comparison

The benchmark matrix uses `ServeHTTP` with a no-op response writer and shared generated route sets.

### 8192 Routes

```text
BenchmarkRouterImplementations/radix/routes=8192/static-16        	23012708	        52.07 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/static-16        	23142879	        51.68 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/static-16        	22087122	        52.67 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/static-16        	23259464	        51.20 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/static-16        	20367566	        52.36 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/param-16         	11424360	       110.1 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/param-16         	10986062	       116.7 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/param-16         	11075352	       112.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/param-16         	10157044	       109.3 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/param-16         	10568188	       109.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/deep-param-16    	 6185959	       196.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/deep-param-16    	 6200488	       189.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/deep-param-16    	 6229465	       189.4 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/deep-param-16    	 6195766	       191.0 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/deep-param-16    	 5933913	       193.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/wildcard-16      	11029969	       104.3 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/wildcard-16      	11537984	       104.7 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/wildcard-16      	10440147	       109.4 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/wildcard-16      	11600074	       103.4 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/radix/routes=8192/wildcard-16      	11692635	       103.7 ns/op	       0 B/op	       0 allocs/op

BenchmarkRouterImplementations/compiled/routes=8192/static-16     	18751464	        55.58 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/static-16     	21268072	        56.60 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/static-16     	20992232	        55.54 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/static-16     	19035955	        55.65 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/static-16     	21527829	        59.22 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/param-16      	10772127	       111.2 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/param-16      	11032189	       109.1 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/param-16      	10906790	       108.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/param-16      	11454064	       104.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/param-16      	11174151	       104.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/deep-param-16 	 7159899	       172.2 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/deep-param-16 	 6634129	       173.1 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/deep-param-16 	 6967774	       167.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/deep-param-16 	 6644907	       182.1 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/deep-param-16 	 7051974	       168.7 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/wildcard-16   	12126662	       100.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/wildcard-16   	11597766	       102.3 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/wildcard-16   	12291856	       106.8 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/wildcard-16   	12314726	       103.8 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/compiled/routes=8192/wildcard-16   	11176076	       103.6 ns/op	       0 B/op	       0 allocs/op

BenchmarkRouterImplementations/matchit/routes=8192/static-16      	21252214	        52.29 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/static-16      	24093433	        53.13 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/static-16      	28214314	        39.01 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/static-16      	31153617	        39.20 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/static-16      	32039472	        38.92 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/param-16       	 7560597	       171.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/param-16       	 4958062	       230.0 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/param-16       	 5637163	       218.2 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/param-16       	 5534358	       212.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/param-16       	 5316841	       214.8 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/deep-param-16  	 3642085	       322.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/deep-param-16  	 3675493	       335.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/deep-param-16  	 3408338	       339.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/deep-param-16  	 3438342	       333.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/deep-param-16  	 3403836	       323.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/wildcard-16    	 5326394	       226.4 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/wildcard-16    	 5318108	       225.2 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/wildcard-16    	 5284438	       226.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/wildcard-16    	 5212230	       225.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterImplementations/matchit/routes=8192/wildcard-16    	 5187109	       224.2 ns/op	       0 B/op	       0 allocs/op

BenchmarkRouterImplementations/std/routes=8192/param-16           	 1681629	       741.6 ns/op	     720 B/op	       5 allocs/op
BenchmarkRouterImplementations/std/routes=8192/param-16           	 1676169	       756.1 ns/op	     720 B/op	       5 allocs/op
BenchmarkRouterImplementations/std/routes=8192/param-16           	 1616965	       718.2 ns/op	     720 B/op	       5 allocs/op
BenchmarkRouterImplementations/std/routes=8192/param-16           	 1649149	       785.7 ns/op	     720 B/op	       5 allocs/op
BenchmarkRouterImplementations/std/routes=8192/param-16           	 1542058	       761.1 ns/op	     720 B/op	       5 allocs/op
```

## Recommendation

## Radix Hybrid Static Children Update

`RadixRouter` was updated to store low-fanout static children inline and only
build a map index when a node reaches `radixStaticIndexThreshold` children. This
keeps the public routing behavior unchanged and preserves zero allocations on
the hot path.

Additional commands:

```powershell
go test ./ghttp -run 'TestRadixTree(KeepsSmallStaticFanoutInline|BuildsStaticIndexForLargeFanout)'
go test ./ghttp
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=5
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=3
```

All commands exited with status 0.

### Focused Radix Lookup, 8192 Routes

| Case | Before avg | After avg | Change |
| --- | ---: | ---: | ---: |
| static | 38.1 ns/op | 25.5 ns/op | 33.1% faster |
| param | 98.2 ns/op | 64.0 ns/op | 34.9% faster |
| deep-param | 199.1 ns/op | 118.0 ns/op | 40.7% faster |
| wildcard | 85.4 ns/op | 59.6 ns/op | 30.3% faster |

### ServeHTTP Implementation Matrix, 8192 Routes

| Case | Old Radix avg | New Radix avg | Compiled avg | New Radix vs Old Radix | New Radix vs Compiled |
| --- | ---: | ---: | ---: | ---: | ---: |
| static | 52.0 ns/op | 39.4 ns/op | 39.5 ns/op | 24.2% faster | 0.3% faster |
| param | 111.8 ns/op | 76.5 ns/op | 79.5 ns/op | 31.6% faster | 3.8% faster |
| deep-param | 192.0 ns/op | 126.3 ns/op | 127.1 ns/op | 34.2% faster | 0.6% faster |
| wildcard | 105.1 ns/op | 76.3 ns/op | 76.9 ns/op | 27.4% faster | 0.8% faster |

## Radix Single Param Tree Update

`RadixRouter` was updated again to store all non-wildcard parameter routes for
one method in a single `paramTree` instead of selecting a tree through
`pathSegmentCount(path)` before lookup. Exact path semantics remain enforced by
the radix traversal: a route only matches when the request path ends on a node
with an entry.

Additional commands:

```powershell
go test ./ghttp -run 'TestMethodMatcherStoresParamRoutesInSingleTree'
go test ./ghttp -run 'TestRadix'
go test ./ghttp -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRadixRouter(Lookup|ServeHTTPNoopWriter)' -benchmem -count=5
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations' -benchmem -count=3
```

All commands exited with status 0.

### Focused Radix Lookup, 8192 Routes

| Case | Hybrid children avg | Single param tree avg | Change |
| --- | ---: | ---: | ---: |
| param | 64.0 ns/op | 63.0 ns/op | 1.6% faster |
| deep-param | 118.0 ns/op | 100.4 ns/op | 14.9% faster |
| wildcard | 59.6 ns/op | 56.7 ns/op | 4.9% faster |
| miss | 37.7 ns/op | 33.4 ns/op | 11.4% faster |

### ServeHTTP Implementation Matrix, 8192 Routes

| Case | Hybrid children Radix avg | Single param tree Radix avg | Compiled avg | Single param tree vs Hybrid | Single param tree vs Compiled |
| --- | ---: | ---: | ---: | ---: | ---: |
| static | 39.4 ns/op | 40.2 ns/op | 39.1 ns/op | 2.0% slower | 2.8% slower |
| param | 76.5 ns/op | 66.4 ns/op | 78.7 ns/op | 13.2% faster | 15.6% faster |
| deep-param | 126.3 ns/op | 106.5 ns/op | 128.0 ns/op | 15.7% faster | 16.8% faster |
| wildcard | 76.3 ns/op | 67.9 ns/op | 77.0 ns/op | 11.0% faster | 11.8% faster |

## Radix vs Compiled Multi-Run Confirmation

The final comparison was repeated with longer per-sample timing to reduce
short-run scheduling noise:

```powershell
go test ./ghttp -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations/(radix|compiled)/routes=8192' -benchmem -count=10
go test ./ghttp -run '^$' -bench 'BenchmarkRouterImplementations/(radix|compiled)/routes=8192' -benchmem -benchtime=3s -count=5
```

All commands exited with status 0. The `-benchtime=3s` run is the most stable
final data point:

| Case | Radix avg | Compiled avg | Result |
| --- | ---: | ---: | ---: |
| static | 53.72 ns/op | 58.10 ns/op | Radix 7.5% faster |
| param | 96.06 ns/op | 119.36 ns/op | Radix 19.5% faster |
| deep-param | 167.08 ns/op | 195.14 ns/op | Radix 14.4% faster |
| wildcard | 99.19 ns/op | 115.04 ns/op | Radix 13.8% faster |

A shorter `128/1024/8192` matrix showed high variance on this Windows laptop,
including conflicting 8192-route static and param results. Treat long
single-case runs or `benchstat` output as the stronger evidence for future
router comparisons.

## Updated Recommendation

Keep `RadixRouter` as the default. After the hybrid static child update and
single param tree update it is still allocation-free and now beats the
standalone `CompiledRouter` experiment across static, parameter,
deep-parameter, and wildcard routes in the long-sample 8192-route matrix.

Keep `CompiledRouter` as an experiment only if it is useful for future design
comparison. Its previous deep-parameter advantage disappears once the default
Radix implementation avoids map lookups on small fanout nodes and avoids
pre-scanning parameter paths for segment counts.

Do not continue the standalone `MatchitRouter` prototype as a default-router candidate for this route shape. It is allocation-free and close on static full-path routes, but its byte-prefix traversal is substantially slower for segment-heavy REST parameter and wildcard routes.

`StdRouter` remains useful as a compatibility adapter, not a performance candidate for parameterized routes.
