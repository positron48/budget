"""Conservative quantity reconstruction within a supplied broker snapshot."""
from datetime import datetime
from decimal import Decimal, localcontext


def position_before(operations, event):
    """This proves arithmetic over supplied history, never snapshot completeness."""
    at = datetime.fromisoformat(event['date'].replace('Z', '+00:00'))
    signed = {'OPERATION_TYPE_BUY':1, 'OPERATION_TYPE_BUY_CARD':1,
              'OPERATION_TYPE_SELL':-1, 'OPERATION_TYPE_SELL_CARD':-1}
    cash_only = {'OPERATION_TYPE_COUPON','OPERATION_TYPE_BOND_TAX',
                 'OPERATION_TYPE_BROKER_FEE','OPERATION_TYPE_DIVIDEND_TAX',
                 'OPERATION_TYPE_DIVIDEND','OPERATION_TYPE_OVERNIGHT'}
    result = {'state':'unresolved','quantity':None,'operation_ids':[],
              'issues':[], 'history_completeness_confirmed':False}
    seen, events = set(), []
    with localcontext() as ctx:
        ctx.prec = 1024
        for op in operations:
            if (op.get('brokerAccountId') != event.get('brokerAccountId')
                    or op.get('instrumentUid') != event.get('instrumentUid')
                    or op.get('state') != 'OPERATION_STATE_EXECUTED' or op.get('id') == event.get('id')):
                continue
            when = datetime.fromisoformat(op['date'].replace('Z', '+00:00'))
            if when > at or op.get('type') in cash_only:
                continue
            if when == at:
                result['issues'].append('same_time_security_event');continue
            identity = op.get('id')
            if not identity or identity in seen:
                result['issues'].append('duplicate_or_missing_operation_id');continue
            seen.add(identity)
            if op.get('type') not in signed:
                result['issues'].append('security_event_requires_review');continue
            try:
                quantity = Decimal(str(op['quantityDone']))
                if not quantity.is_finite() or quantity <= 0:
                    raise ValueError()
            except (ValueError,KeyError,ArithmeticError):
                result['issues'].append('invalid_executed_quantity');continue
            events.append((when, identity, quantity * signed[op['type']]))
        balance = Decimal(0)
        for when in sorted({e[0] for e in events}):
            group = sorted((e for e in events if e[0]==when),key=lambda e:e[1])
            balance += sum((e[2] for e in group),Decimal(0))
            result['operation_ids'].extend(e[1] for e in group)
            if balance < 0:
                result['issues'].append('negative_position_in_supplied_history')
        result['issues'] = sorted(set(result['issues']))
        if not result['issues']:
            result['state']='reconstructed_from_supplied_history'
            result['quantity']=format(balance,'f')
    return result
