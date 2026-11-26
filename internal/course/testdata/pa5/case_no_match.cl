class A {};
class Main inherits IO {
  main() : Object { case new A of i : Int => out_string("int\n"); s : String => out_string("string\n"); esac };
};
