import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/db/call_log_entry.dart';
import '../../core/phone/number_format.dart';
import '../contacts/contact_edit_screen.dart';

class HistoryScreen extends StatelessWidget {
  const HistoryScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final entries = appState.callHistory;

    if (entries.isEmpty) {
      return Scaffold(
        appBar: AppBar(title: const Text('History')),
        body: Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(Icons.history, size: 64, color: Colors.grey[400]),
              const SizedBox(height: 12),
              Text(
                'No calls yet',
                style: TextStyle(color: Colors.grey[600]),
              ),
              const SizedBox(height: 4),
              Text(
                'Calls you make and receive will show up here',
                style: TextStyle(color: Colors.grey[500], fontSize: 13),
              ),
            ],
          ),
        ),
      );
    }

    final grouped = _groupByDay(entries);
    return Scaffold(
      appBar: AppBar(
        title: const Text('History'),
        actions: [
          IconButton(
            tooltip: 'Clear history',
            icon: const Icon(Icons.delete_sweep_outlined),
            onPressed: () => _confirmClear(context, appState),
          ),
        ],
      ),
      body: ListView(
        children: [
          for (final group in grouped) ...[
            _DayHeader(label: group.dayLabel),
            for (final entry in group.entries)
              _HistoryRow(entry: entry),
          ],
        ],
      ),
    );
  }

  Future<void> _confirmClear(BuildContext context, AppState appState) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Clear history'),
        content: const Text('Remove all call history?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Clear', style: TextStyle(color: Colors.red)),
          ),
        ],
      ),
    );
    if (ok == true) {
      await appState.clearCallHistory();
    }
  }

  List<_DayGroup> _groupByDay(List<CallLogEntry> entries) {
    final now = DateTime.now();
    final today = DateTime(now.year, now.month, now.day);
    final yesterday = today.subtract(const Duration(days: 1));

    String labelFor(DateTime t) {
      final d = DateTime(t.year, t.month, t.day);
      if (d == today) return 'Today';
      if (d == yesterday) return 'Yesterday';
      return _formatDate(t);
    }

    final groups = <String, List<CallLogEntry>>{};
    for (final e in entries) {
      groups.putIfAbsent(labelFor(e.startedAt), () => []).add(e);
    }
    return groups.entries
        .map((g) => _DayGroup(dayLabel: g.key, entries: g.value))
        .toList();
  }
}

class _DayGroup {
  final String dayLabel;
  final List<CallLogEntry> entries;
  _DayGroup({required this.dayLabel, required this.entries});
}

class _DayHeader extends StatelessWidget {
  final String label;
  const _DayHeader({required this.label});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      color: Colors.grey[200],
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      child: Text(
        label,
        style: TextStyle(
          color: Colors.grey[700],
          fontSize: 13,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}

class _HistoryRow extends StatelessWidget {
  final CallLogEntry entry;
  const _HistoryRow({required this.entry});

  IconData get _icon {
    switch (entry.type) {
      case CallType.outgoing:
        return Icons.call_made;
      case CallType.incoming:
        return Icons.call_received;
      case CallType.missed:
        return Icons.call_missed;
      case CallType.rejected:
        return Icons.call_end;
    }
  }

  Color get _color {
    switch (entry.type) {
      case CallType.outgoing:
        return Colors.blueGrey;
      case CallType.incoming:
        return Colors.green;
      case CallType.missed:
        return Colors.red;
      case CallType.rejected:
        return Colors.redAccent;
    }
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final isService = isServiceCode(entry.number);

    String title;
    String subtitle;
    if (isService) {
      title = entry.serviceName ?? 'Service';
      subtitle = entry.number;
    } else {
      final numberShown = displayNumber(entry.number);
      final contact = appState.contactForNumber(entry.number);
      if (contact != null) {
        title = contact.name;
        subtitle = numberShown;
      } else if (entry.nameSnapshot != null &&
          entry.nameSnapshot!.isNotEmpty) {
        title = entry.nameSnapshot!;
        subtitle = numberShown;
      } else {
        title = numberShown;
        subtitle = '';
      }
      if (entry.durationSec > 0) {
        subtitle = subtitle.isEmpty
            ? _formatDuration(entry.durationSec)
            : '$subtitle · ${_formatDuration(entry.durationSec)}';
      }
    }

    return ListTile(
      leading: Icon(_icon, color: _color),
      title: Text(
        title,
        style: TextStyle(
          fontWeight:
              entry.type == CallType.missed ? FontWeight.w600 : FontWeight.normal,
        ),
      ),
      subtitle: subtitle.isEmpty ? null : Text(subtitle),
      trailing: Text(_formatClock(entry.startedAt),
          style: TextStyle(color: Colors.grey[600], fontSize: 12)),
      onTap: () =>
          appState.makeCall(dialFromCanonical(entry.number)),
      onLongPress: () => _showActions(context, appState, isService),
    );
  }

  void _showActions(
    BuildContext context,
    AppState appState,
    bool isService,
  ) async {
    final addContactAvailable =
        !isService && appState.contactForNumber(entry.number) == null;
    final action = await showModalBottomSheet<String>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (addContactAvailable)
              ListTile(
                leading: const Icon(Icons.person_add),
                title: const Text('Add to contacts'),
                onTap: () => Navigator.pop(ctx, 'add'),
              ),
            ListTile(
              leading: const Icon(Icons.delete_outline, color: Colors.red),
              title: const Text('Delete entry',
                  style: TextStyle(color: Colors.red)),
              onTap: () => Navigator.pop(ctx, 'delete'),
            ),
          ],
        ),
      ),
    );

    if (!context.mounted) return;
    switch (action) {
      case 'add':
        await Navigator.of(context).push(
          MaterialPageRoute(
            builder: (_) => ContactEditScreen(
              initialNumber: entry.number,
            ),
          ),
        );
      case 'delete':
        await appState.deleteHistoryEntry(entry.id);
    }
  }
}

String _formatDuration(int sec) {
  final m = (sec ~/ 60).toString().padLeft(2, '0');
  final s = (sec % 60).toString().padLeft(2, '0');
  return '$m:$s';
}

String _formatClock(DateTime t) {
  final h = t.hour.toString().padLeft(2, '0');
  final m = t.minute.toString().padLeft(2, '0');
  return '$h:$m';
}

String _formatDate(DateTime t) {
  const months = [
    'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
    'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
  ];
  return '${months[t.month - 1]} ${t.day}, ${t.year}';
}
