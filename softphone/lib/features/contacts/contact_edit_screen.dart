import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/db/contact.dart';
import '../../core/phone/number_format.dart';

/// Create (contact == null) or edit a contact.
class ContactEditScreen extends StatefulWidget {
  final Contact? contact;

  /// Pre-fill the number field when creating a new contact (e.g. from the
  /// history screen).
  final String? initialNumber;

  const ContactEditScreen({super.key, this.contact, this.initialNumber});

  @override
  State<ContactEditScreen> createState() => _ContactEditScreenState();
}

class _ContactEditScreenState extends State<ContactEditScreen> {
  final _nameController = TextEditingController();
  final _numberController = TextEditingController();
  bool _saving = false;
  String? _error;

  bool get _isNew => widget.contact == null;

  @override
  void initState() {
    super.initState();
    final c = widget.contact;
    if (c != null) {
      _nameController.text = c.name;
      _numberController.text = c.number;
    } else if (widget.initialNumber != null) {
      _numberController.text = widget.initialNumber!;
    }
  }

  @override
  void dispose() {
    _nameController.dispose();
    _numberController.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final appState = context.read<AppState>();
    final name = _nameController.text.trim();
    final number = _numberController.text.trim();
    final canon = canonicalNumber(
      number,
      ownArea: appState.phoneNumber?.areaCode,
    );

    if (name.isEmpty) {
      setState(() => _error = 'Enter a name.');
      return;
    }
    final validLength = canon.length == 7 || canon.length == 10;
    if (canon.isEmpty || (!validLength && !isServiceCode(canon))) {
      setState(() => _error = 'Enter a valid OCN number '
          '(7-digit local or full 10-digit).');
      return;
    }

    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final c = widget.contact;
      if (c == null) {
        await appState.addContact(name: name, number: canon);
      } else {
        await appState.updateContact(c, name: name, number: canon);
      }
      if (mounted) Navigator.of(context).pop();
    } catch (e) {
      setState(() {
        _saving = false;
        _error = 'Save failed: $e';
      });
    }
  }

  Future<void> _confirmDelete() async {
    final c = widget.contact;
    if (c == null) return;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete contact'),
        content: Text('Remove ${c.name}?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete', style: TextStyle(color: Colors.red)),
          ),
        ],
      ),
    );
    if (ok == true && mounted) {
      await context.read<AppState>().deleteContact(c.id);
      if (mounted) Navigator.of(context).pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(_isNew ? 'New contact' : 'Edit contact'),
        actions: [
          if (!_isNew)
            IconButton(
              tooltip: 'Delete',
              icon: const Icon(Icons.delete_outline),
              onPressed: _saving ? null : _confirmDelete,
            ),
        ],
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextField(
              controller: _nameController,
              enabled: !_saving,
              textCapitalization: TextCapitalization.words,
              decoration: const InputDecoration(
                labelText: 'Name',
                hintText: 'Jane Doe',
                border: OutlineInputBorder(),
                prefixIcon: Icon(Icons.person_outline),
              ),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _numberController,
              enabled: !_saving,
              keyboardType: TextInputType.phone,
              inputFormatters: [
                FilteringTextInputFormatter.allow(RegExp(r'[0-9*#-]')),
              ],
              decoration: const InputDecoration(
                labelText: 'Number',
                hintText: '5551234 or 4405551234',
                border: OutlineInputBorder(),
                prefixIcon: Icon(Icons.phone_outlined),
              ),
            ),
            const SizedBox(height: 8),
            Text(
              'Local (7-digit) or full (10-digit) OCN numbers.',
              style: TextStyle(color: Colors.grey[600], fontSize: 12),
            ),
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(
                _error!,
                style: const TextStyle(color: Colors.red, fontSize: 13),
              ),
            ],
            const SizedBox(height: 24),
            SizedBox(
              height: 48,
              child: ElevatedButton(
                onPressed: _saving ? null : _save,
                child: _saving
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Text(_isNew ? 'Save' : 'Save changes'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
