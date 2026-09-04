import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/db/contact.dart';
import '../../core/phone/number_format.dart';
import 'contact_edit_screen.dart';

class ContactsScreen extends StatelessWidget {
  const ContactsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final contacts = [...appState.contacts]
      ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));

    return Scaffold(
      appBar: AppBar(title: const Text('Contacts')),
      floatingActionButton: FloatingActionButton(
        heroTag: 'contacts_add',
        tooltip: 'Add contact',
        onPressed: () {
          Navigator.of(context).push(
            MaterialPageRoute(
              builder: (_) => const ContactEditScreen(),
            ),
          );
        },
        child: const Icon(Icons.person_add),
      ),
      body: contacts.isEmpty
          ? const _EmptyContacts()
          : ListView.separated(
              itemCount: contacts.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (context, i) =>
                  _ContactRow(contact: contacts[i]),
            ),
    );
  }
}

class _EmptyContacts extends StatelessWidget {
  const _EmptyContacts();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.contacts_outlined, size: 64, color: Colors.grey[400]),
          const SizedBox(height: 12),
          Text('No contacts yet', style: TextStyle(color: Colors.grey[600])),
          const SizedBox(height: 4),
          Text(
            'Tap + to add a contact',
            style: TextStyle(color: Colors.grey[500], fontSize: 13),
          ),
        ],
      ),
    );
  }
}

class _ContactRow extends StatelessWidget {
  final Contact contact;

  const _ContactRow({required this.contact});

  Future<void> _confirmDelete(BuildContext context) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete contact'),
        content: Text('Remove ${contact.name}?'),
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
    if (ok == true && context.mounted) {
      await context.read<AppState>().deleteContact(contact.id);
    }
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.read<AppState>();
    final initial =
        contact.name.trim().isEmpty ? '?' : contact.name.trim()[0].toUpperCase();

    return ListTile(
      leading: CircleAvatar(child: Text(initial)),
      title: Text(contact.name),
      subtitle: Text(displayNumber(contact.number)),
      onTap: () => appState.makeCall(dialFromCanonical(contact.number)),
      trailing: PopupMenuButton<String>(
        onSelected: (value) async {
          switch (value) {
            case 'edit':
              await Navigator.of(context).push(
                MaterialPageRoute(
                  builder: (_) => ContactEditScreen(contact: contact),
                ),
              );
              break;
            case 'delete':
              await _confirmDelete(context);
              break;
          }
        },
        itemBuilder: (_) => const [
          PopupMenuItem(value: 'edit', child: Text('Edit')),
          PopupMenuItem(value: 'delete', child: Text('Delete')),
        ],
      ),
    );
  }
}
