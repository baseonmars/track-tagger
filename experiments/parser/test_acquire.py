import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('acquire',Path(__file__).with_name('acquire.py'))
acquire = importlib.util.module_from_spec(spec)
spec.loader.exec_module(acquire)


class AcquisitionTests(unittest.TestCase):
    def metadata(self):
        self.data = b'Artist\n2020-01-02\n01 Song\n'
        return {'metadata':{'creator':'Synthetic Artist','date':'2020-01-02','uploader':'fixture'},'files':[
            {'name':'disc 1/show.txt','source':'original','size':str(len(self.data)),'md5':hashlib.md5(self.data).hexdigest()},
            {'name':'disc 1/show01.flac','source':'original','format':'Flac','size':'100'},
            {'name':'disc 1/show01.mp3','source':'derivative','size':'20'},
            {'name':'extra.nfo','source':'original','size':str(len(self.data))}]}

    def test_original_inventory_and_all_notes(self):
        notes,audio = acquire.select_files(self.metadata())
        self.assertEqual([n['name'] for n in notes],['disc 1/show.txt','extra.nfo'])
        self.assertEqual([a['name'] for a in audio],['disc 1/show01.flac'])

    def test_reject_missing_and_oversize(self):
        for files in ([],[{'name':'x.flac','source':'original'}],[{'name':'x.txt','source':'original','size':acquire.MAX_NOTE+1}]):
            with self.assertRaises(ValueError):
                acquire.select_files({'files':files})

    def test_path_safety(self):
        for path in ('../x.txt','/x.txt','a/../../x.txt','a\\x.txt','\x00'):
            with self.assertRaises(ValueError):
                acquire.safe_relative(path)
        self.assertEqual(acquire.safe_relative('disc 1/x.txt'),'disc 1/x.txt')

    def test_decode_original_bytes(self):
        self.assertEqual(acquire.decode_note('Café'.encode('cp1252')),('Café','cp1252'))
        self.assertEqual(acquire.decode_note('Café'.encode('utf-16'))[0],'Café')
        with self.assertRaises(ValueError):
            acquire.decode_note(b'\x00binary')

    def test_acquire_complete_cache_and_integrity(self):
        metadata = self.metadata()
        calls = []
        def fake_fetch(url,limit=0):
            calls.append(url)
            return json.dumps(metadata).encode() if '/metadata/' in url else self.data
        with tempfile.TemporaryDirectory() as temporary, patch.object(acquire,'fetch',fake_fetch):
            root = Path(temporary)
            package = acquire.acquire('fixture-1',root)
            self.assertEqual(len(package['notes']),2)
            self.assertEqual(len(package['audio_files']),1)
            self.assertTrue(package['complete'])
            self.assertTrue(all(not u.endswith('.flac') for u in calls))
            count = len(calls)
            self.assertEqual(acquire.acquire('fixture-1',root),package)
            self.assertEqual(len(calls),count)
            (root/'packages/fixture-1/notes/extra.nfo').write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError,'cached note hash'):
                acquire.acquire('fixture-1',root)

    def test_cached_metadata_tamper_is_rejected(self):
        metadata = self.metadata()
        with tempfile.TemporaryDirectory() as temporary, patch.object(acquire,'fetch',lambda u,limit=0:json.dumps(metadata).encode() if '/metadata/' in u else self.data):
            root = Path(temporary)
            acquire.acquire('fixture-4',root)
            (root/'packages/fixture-4/archive-metadata.json').write_text('{}')
            with self.assertRaisesRegex(ValueError,'metadata hash'):
                acquire.acquire('fixture-4',root)

    def test_incomplete_download_never_publishes_package(self):
        metadata = self.metadata()
        def fake_fetch(url,limit=0):
            return json.dumps(metadata).encode() if '/metadata/' in url else b'truncated'
        with tempfile.TemporaryDirectory() as temporary, patch.object(acquire,'fetch',fake_fetch):
            root = Path(temporary)
            with self.assertRaisesRegex(ValueError,'size mismatch'):
                acquire.acquire('fixture-2',root)
            self.assertFalse((root/'packages/fixture-2/package.json').exists())

    def test_public_manifest_excludes_freeform_text(self):
        metadata = self.metadata()
        with tempfile.TemporaryDirectory() as temporary, patch.object(acquire,'fetch',lambda u,limit=0:json.dumps(metadata).encode() if '/metadata/' in u else self.data):
            record = acquire.public_record(acquire.acquire('fixture-3',Path(temporary)))
            self.assertNotIn('reference_metadata',record)
            self.assertNotIn('text',json.dumps(record))

class NormalizationAndGroupingTests(unittest.TestCase):
    def case(self,ident,text,artist='Synthetic',date='2020-01-02'):
        return {'id':ident,'notes':[{'name':'notes.txt','text':text}], 'provenance':{'artist':artist,'date':date}}

    def test_alternate_performance_and_exact_text_group(self):
        cases = [self.case('a','One'),self.case('b','Different'),self.case('c','one',artist='Other',date='2021-01-01'),self.case('d','Distinct',artist='Other',date='2022-01-01')]
        acquire.group_packages(cases)
        self.assertEqual(cases[0]['group'],cases[1]['group'])
        self.assertEqual(cases[0]['group'],cases[2]['group'])
        self.assertNotEqual(cases[0]['group'],cases[3]['group'])

    def test_near_duplicate_notes_group(self):
        text = ' '.join('word'+chr(97+i//26)+chr(97+i%26) for i in range(100))
        cases = [self.case('a',text),self.case('b',text+' extra words',artist='Other')]
        acquire.group_packages(cases)
        self.assertEqual(cases[0]['group'],cases[1]['group'])

    def test_template_similarity_groups_different_track_titles(self):
        template = 'Source recording microphone transfer lineage technical details ' + ' '.join('token'+chr(97+i//26)+chr(97+i%26) for i in range(80))
        cases = [self.case('a',template+'\n01. First song\n02. Second song'),self.case('b',template+'\n01. Different song\n02. Another song',artist='Other')]
        acquire.group_packages(cases)
        self.assertEqual(cases[0]['group'],cases[1]['group'])

    def test_group_membership_is_stable_under_input_reorder(self):
        import copy
        cases = [self.case('a','One'),self.case('b','Different'),self.case('c','one',artist='Other',date='2021-01-01')]
        reversed_cases = copy.deepcopy(list(reversed(cases)))
        acquire.group_packages(cases)
        acquire.group_packages(reversed_cases)
        self.assertEqual({c['id']:c['group'] for c in cases},{c['id']:c['group'] for c in reversed_cases})

    def test_short_near_duplicate_with_same_structure_groups(self):
        text = 'Source recording taper lineage microphone transfer normalized archival details concert recording source equipment technical notes audience recording fixture one'
        cases = [self.case('a',text),self.case('b',text[:-3]+'two',artist='Other')]
        acquire.group_packages(cases)
        self.assertEqual(cases[0]['group'],cases[1]['group'])

    def test_short_boilerplate_does_not_group(self):
        cases = [self.case('a','Thanks to the taper. Song A'),self.case('b','Thanks to the taper. Song B',artist='Other')]
        acquire.group_packages(cases)
        self.assertNotEqual(cases[0]['group'],cases[1]['group'])

    def test_partition_audit_exposes_cross_partition_group_and_text(self):
        cases = [self.case('a','one two three four five six seven eight nine'),self.case('b','one two three four five six seven eight nine',artist='Other')]
        acquire.group_packages(cases)
        cases[0]['split'],cases[1]['split'] = 'train','test'
        audit = acquire.audit_grouping(cases)
        self.assertEqual(audit['cross_partition_group_violations'],1)
        self.assertEqual(audit['max_cross_partition_note_similarity']['jaccard'],1)
        self.assertEqual(audit['max_cross_partition_template_similarity']['jaccard'],1)

    def test_normalize_integrity_utf8_and_applicability(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            directory = root/'packages/test'
            directory.mkdir(parents=True)
            data = 'Café'.encode('cp1252')
            (directory/'original.nfo').write_bytes(data)
            package = {'package_id':'test','artist':'Fixture','date':'2001-01-01','source':{'url':'https://example.invalid'},'archive_metadata_sha256':'hash','notes':[{'path':'original.nfo','local_path':'original.nfo','sha256':acquire.digest(data)}],'audio_files':[{'path':'track.shn'}]}
            acquire.atomic_json(directory/'package.json',package)
            case = list(acquire.normalize_packages(root))[0]
            self.assertEqual(case['notes'][0]['text'],'Café')
            self.assertEqual(case['notes'][0]['encoding'],'utf-8')
            self.assertEqual(case['notes'][0]['source_encoding'],'cp1252')
            self.assertIn('non-flac-original-audio',case['features'])
            self.assertIn('unsupported-original-audio',case['features'])
            self.assertNotIn('unsupported-note-name',case['features'])
            self.assertIsNone(case['oracle'])

if __name__ == '__main__':
    unittest.main()

class AliasGroupingTests(unittest.TestCase):
    def test_creator_alias_intersection_groups_full_performance_date(self):
        cases=[{'id':'a','notes':[{'name':'a','text':'one source completely distinct'}],'provenance':{'artist':'Smashing Pumpkins; The Smashing Pumpkins','date':'1994-03-10'}},
               {'id':'b','notes':[{'name':'a','text':'another unrelated recording source'}],'provenance':{'artist':'Smashing Pumpkins','date':'1994-03-10'}}]
        acquire.group_packages(cases)
        self.assertEqual(cases[0]['group'],cases[1]['group'])
        cases[0]['provenance']['date']='1994';cases[1]['provenance']['date']='1994'
        acquire.group_packages(cases)
        self.assertNotEqual(cases[0]['group'],cases[1]['group'])
