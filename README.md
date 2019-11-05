# Balero Lambda

> Let me know when to catch the least crowded bart trains

> Work in Progress

**Configuration details**
- Lambda role needs to have SNS:Publish on resource * 
- Pinpoint number needs to be set up for 2-way SMS and pipe into SNS
- SNS should trigger lambda

perms:
lambda role
-sns publish
-dynamodb read/write table

