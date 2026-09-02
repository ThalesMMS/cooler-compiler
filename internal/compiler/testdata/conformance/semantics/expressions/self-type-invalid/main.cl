class Parent {
  cached : SELF_TYPE;
  bad_formal(x : SELF_TYPE) : Object { 0 };
  bad_return() : SELF_TYPE { new Child };
  bad_assignment() : Object { cached <- new Child };
};
class Child inherits Parent {};
class Main {
  invalid_static() : Object { (new Child)@SELF_TYPE.copy() };
  invalid_case() : Object { case new Child of x : SELF_TYPE => x; esac };
  main() : Object { 0 };
};
