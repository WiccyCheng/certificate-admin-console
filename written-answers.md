1. Because of the rolling deployment, an old application may find users.display_name column gone once the data schema change completes. The fundemantel error here, is that there 
is no "rolling" status in data schema change, either change is done or not. Also this has to be transparent to application. I think the safe way to do is, first we stop the service
at the minimum requests moment(let say midnight) for about 1 hour, then add new column users.full_name, in the meantime keep users.display_name for rollback, second we upgrade all application.
If rolling deployment is must, we will have to make users.display_name into a shards service. Which making the rolling scope predicitable, let say the upgrade starts with the shard that
contains users.display_name begin with "A", we will do almost the same as the previous "stop then upgrade" plan, but difference is that the other reads and writes request that not fall to
shard "A", will continue go to the users.display_name service. And for shard "A" we will have to options, either we have the service stop(since it only affects username start with A users)
or we can make the shard service some replica, during the upgrade, reads request go to replica and write request may stuck or write into cache(and have cache applied once the upgrade is finished
may have data loss situation though).

2. This error happens because the operation read database then write cache is not atomic, thus if B revoked certificate after A reading it frome database and before A writing it to cache,
this error happens. We can have revoke operation (and other write request) update all relevant cache status, but still there might be someone just read the stale data before B updating the cache,
if 100% accurecy is required, all write request need to 
  a. lock certificate A
  b. notify all cache that certificate A is stale
  c. update status of certificate A
  d. (optional) notify certificate A status to cache

3. As B's latency rises up, workers will stuck up to 6s(3 times retries) in current /report designs, with 1,000 requests per second, workers will immetediately runs out in service A, all of them are waiting
service B to response. Thus, other unrelated endpoint stop responding. I think the core error here, is that service A have a shared workers pool for all downstream service, any of downstream service crashed
or lantency rises up will affect service A. So first, shared workers pool can keep, but have to set a limitation for each services or just for downstream services. Or set a setperate workers pool for downstream
services. Secondly, the retry time must be exponential backoff. Lastly, /report should return timeout and service A may do some rate limiting, once observe service B stuck limit amount of workers, reject most
following /report requests.
