import unittest
from .components import verify_split

def record(name,uploader,technical=''):
    return {'package_id':name,'artist':name,'date':'2001-01-01','reference_metadata':{'uploader':uploader}},name+'\n'+technical

class ComponentTests(unittest.TestCase):
    def test_uploader_edge_rejects_crossing(self):
        rs={'a':record('a','same'),'b':record('b','same')}
        with self.assertRaisesRegex(ValueError,'component crosses'):verify_split(rs,{'a':'train','b':'test'},{},[])
    def test_distinctive_template_edge_rejects_crossing(self):
        chain='Source recorded using distinctive microphone equipment through original tape recorder'
        rs={'a':record('a','one',chain),'b':record('b','two',chain)}
        with self.assertRaisesRegex(ValueError,'component crosses'):verify_split(rs,{'a':'train','b':'test'},{},[])
    def test_old_uploader_cannot_enter_heldout(self):
        rs={'new':record('new','old')}
        with self.assertRaisesRegex(ValueError,'related to old'):verify_split(rs,{'new':'test'},{'old':record('old','old')},[])
    def test_training_only_cannot_enter_validation(self):
        with self.assertRaisesRegex(ValueError,'training-only'):verify_split({'a':record('a','x')},{'a':'validation'},{},['a'])
    def test_non_latin_performance_alias_is_preserved(self):
        a=record('a','one');b=record('b','two')
        a[0]['artist']='山田太郎';b[0]['artist']='山田 太郎'
        with self.assertRaisesRegex(ValueError,'component crosses'):verify_split({'a':a,'b':b},{'a':'train','b':'test'},{},[])

if __name__=='__main__':unittest.main()
